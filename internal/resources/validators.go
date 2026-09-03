package resources

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// contactURLPattern matches the absolute http(s) URLs the API accepts.
var contactURLPattern = regexp.MustCompile(`^https?://`)

// uniqueStringsValidator rejects a list of strings containing duplicates.
type uniqueStringsValidator struct{}

func (v uniqueStringsValidator) Description(_ context.Context) string {
	return "must not contain duplicate values"
}

func (v uniqueStringsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v uniqueStringsValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	seen := make(map[string]int, len(req.ConfigValue.Elements()))
	for i, elem := range req.ConfigValue.Elements() {
		s, ok := elem.(types.String)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		if first, dup := seen[s.ValueString()]; dup {
			resp.Diagnostics.AddAttributeError(
				req.Path.AtListIndex(i),
				"Duplicate Value",
				fmt.Sprintf("%q is already declared at index %d. Values must be unique.", s.ValueString(), first),
			)
			continue
		}
		seen[s.ValueString()] = i
	}
}

// UniqueStrings returns a validator rejecting duplicate values in a string list.
func UniqueStrings() validator.List {
	return uniqueStringsValidator{}
}

// pngSignature is the 8-byte magic number every PNG file starts with.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// base64PNGValidator rejects strings that are not base64-encoded PNG data, or
// that decode to more than maxBytes.
type base64PNGValidator struct {
	maxBytes int
}

func (v base64PNGValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be a base64-encoded PNG image of at most %d bytes once decoded", v.maxBytes)
}

func (v base64PNGValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v base64PNGValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	encoded := req.ConfigValue.ValueString()

	// Check the encoded length first: base64 inflates by 4/3, so anything longer
	// than that bound cannot fit and there is no reason to materialise it.
	if len(encoded) > base64.StdEncoding.EncodedLen(v.maxBytes) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Image Too Large",
			fmt.Sprintf("Encoded value is %d bytes, which cannot decode to %d bytes or fewer.", len(encoded), v.maxBytes),
		)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Base64",
			fmt.Sprintf("Value must be base64-encoded PNG data: %s.", err),
		)
		return
	}

	if !bytes.HasPrefix(decoded, pngSignature) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid PNG",
			"Decoded value does not start with the PNG signature. Only PNG images are accepted.",
		)
		return
	}

	if len(decoded) > v.maxBytes {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Image Too Large",
			fmt.Sprintf("Decoded image is %d bytes, which exceeds the %d byte limit.", len(decoded), v.maxBytes),
		)
	}
}

// Base64PNG returns a validator accepting only base64-encoded PNG data that
// decodes to at most maxBytes.
func Base64PNG(maxBytes int) validator.String {
	return base64PNGValidator{maxBytes: maxBytes}
}
