package resources

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

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

// crlfJoinedLengthValidator bounds a string list by the length of the CRLF-joined
// blob it becomes, not by the sum of its elements.
//
// That is the shape the API measures: the field is rendered into a textarea, and
// a browser submits one with CRLF line endings, so the server caps
// `records.join("\r\n").length`. Counting the separators matters — against the
// 8192 cap this is wired with, fifty records of 162 characters are 8100
// characters of payload and 8198 of blob, so the 98 characters of separator are
// the whole difference between a config that applies and a 422.
//
// Characters, not bytes: the server's cap is a Ruby String#length, which counts
// codepoints. The implementation mirrors that expression literally — build the
// joined string, count its runes — so the two cannot drift.
type crlfJoinedLengthValidator struct {
	maxChars int
}

func (v crlfJoinedLengthValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be at most %d characters once joined with CRLF line endings", v.maxChars)
}

func (v crlfJoinedLengthValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v crlfJoinedLengthValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	elements := req.ConfigValue.Elements()
	records := make([]string, 0, len(elements))
	for _, elem := range elements {
		s, ok := elem.(types.String)
		// An unknown element makes the total unknowable, and guessing either way
		// is worse than letting the API answer: skip the check entirely.
		if !ok || s.IsUnknown() {
			return
		}
		// A null's ValueString is "", which is exactly what join contributes for
		// it server-side — and the separators around it still count. Building
		// the joined form rather than re-deriving its length is the point: the
		// hand-rolled separator arithmetic this replaces was off by two per null.
		records = append(records, s.ValueString())
	}

	total := utf8.RuneCountInString(strings.Join(records, "\r\n"))
	if total > v.maxChars {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"List Too Long",
			fmt.Sprintf("The records total %d characters once joined with CRLF line endings, "+
				"which exceeds the %d character limit.", total, v.maxChars),
		)
	}
}

// CRLFJoinedLengthAtMost returns a validator bounding a string list by the length
// of its CRLF-joined form, in characters.
func CRLFJoinedLengthAtMost(maxChars int) validator.List {
	return crlfJoinedLengthValidator{maxChars: maxChars}
}

// noControlCharsValidator rejects a NUL byte, carriage return or line feed,
// reporting WHERE the offence is rather than what the value was.
//
// That is the whole reason it exists instead of a stringvalidator.RegexMatches:
// the shared helper behind every library string validator formats
// "Attribute %s %s, got: %s" with the raw value, and the attribute this guards
// is custom_headers — declared Sensitive precisely because it carries an
// Authorization header. Terraform redacts a sensitive value in the plan diff and
// in outputs, but NOT in a provider diagnostic, so a bearer token with a trailing
// newline (file("token") without chomp, an env var, a vault lookup) would print
// itself to stdout and into CI logs on every plan.
type noControlCharsValidator struct {
	// forbidden is the set of bytes rejected, and description names them for the
	// schema docs. A request body legitimately contains newlines, so it forbids
	// the NUL alone; a header value forbids CR and LF too.
	forbidden   string
	description string
}

func (v noControlCharsValidator) Description(_ context.Context) string {
	return "must not contain " + v.description
}

func (v noControlCharsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noControlCharsValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	value := req.ConfigValue.ValueString()
	i := strings.IndexAny(value, v.forbidden)
	if i < 0 {
		return
	}

	name := map[byte]string{0: "NUL byte", '\r': "carriage return", '\n': "line feed"}[value[i]]
	// "at the end" rather than the offset for a trailing character. The offset
	// would be the value's own length, and for the archetypal case this message
	// coaches — a bearer token with a trailing newline — that is a length oracle
	// for the very value the validator exists not to print.
	where := fmt.Sprintf("at byte %d", i)
	if i == len(value)-1 {
		where = "at the end"
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid Character",
		fmt.Sprintf("Value contains a %s %s, which the API rejects. A trailing newline "+
			"from file() or an environment variable is the usual cause — wrap the value in "+
			"chomp(). The value is not echoed here: this validator also guards attributes "+
			"that hold credentials.", name, where),
	)
}

// NoControlCharacters returns a validator rejecting NUL, CR and LF without
// echoing the value into the diagnostic.
func NoControlCharacters() validator.String {
	return noControlCharsValidator{
		forbidden:   "\x00\r\n",
		description: "a NUL byte, carriage return or line feed",
	}
}

// NoNulBytes rejects only the NUL, for a field where newlines are legitimate.
//
// Postgres refuses a NUL inside a text or jsonb column at INSERT — past every
// Rails validation and past the API's error handling, which does not rescue
// StatementInvalid. That makes it a 500 and a pager event for what should be a
// 422, so the provider is the only place it can be caught cheaply.
func NoNulBytes() validator.String {
	return noControlCharsValidator{forbidden: "\x00", description: "a NUL byte"}
}

// noBlankOrPaddedValidator rejects a whitespace-only string, or one with leading
// or trailing whitespace.
//
// For dns_expected_records specifically: neither the API nor the model strips or
// rejects these (only the WEB form does, in dns_expected_records_input=), and the
// probe's comparison does not strip either — it only lowercases and drops a
// trailing dot. So " 1.2.3.4" is stored as an operator-provenanced pin that can
// never match the resolved answer: every DNS event is stamped
// matches_expected: false, and flap damping stays off for good. Terraform
// converges happily on the bad value, so nothing else surfaces it.
type noBlankOrPaddedValidator struct{}

func (v noBlankOrPaddedValidator) Description(_ context.Context) string {
	return "must not be blank or carry leading or trailing whitespace"
}

func (v noBlankOrPaddedValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noBlankOrPaddedValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	value := req.ConfigValue.ValueString()
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		resp.Diagnostics.AddAttributeError(req.Path, "Blank Value",
			"A blank entry is stored as a pin that can never match what DNS resolves, "+
				"which reports a mismatch on every check. Remove the entry — a trailing "+
				"newline in a split() is the usual cause.")
	case trimmed != value:
		resp.Diagnostics.AddAttributeError(req.Path, "Untrimmed Value",
			"Leading or trailing whitespace is stored verbatim and is not stripped before "+
				"the value is compared to what DNS resolves, so this entry can never match. "+
				"Wrap the value in trim().")
	}
}

// NoBlankOrPadded returns a validator rejecting blank or untrimmed strings.
func NoBlankOrPadded() validator.String {
	return noBlankOrPaddedValidator{}
}

// The framework's element validators (ValueStringsAre, and the string validators
// underneath it) all skip a null by contract, so nothing else can catch this: a
// null element passes every check and then marshals as "" in Create/Update,
// which the API stores and echoes back. The plan promised null, the apply wrote
// "", and Terraform aborts with "Provider produced inconsistent result after
// apply". Rejecting it at plan time names the element instead.
const nullElementDetail = "A null entry is sent to the API as an empty string, which it stores. " +
	"Remove the entry, or give it a value."

// noNullElementsValidator rejects a null element inside a list.
type noNullElementsValidator struct{}

func (v noNullElementsValidator) Description(_ context.Context) string {
	return "must not contain a null element"
}

func (v noNullElementsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noNullElementsValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for i, elem := range req.ConfigValue.Elements() {
		if elem.IsNull() {
			resp.Diagnostics.AddAttributeError(req.Path.AtListIndex(i), "Null Element", nullElementDetail)
		}
	}
}

// NoNullElements returns a validator rejecting null elements in a list.
func NoNullElements() validator.List {
	return noNullElementsValidator{}
}

// noNullValuesValidator is NoNullElements for a map's values.
type noNullValuesValidator struct{}

func (v noNullValuesValidator) Description(_ context.Context) string {
	return "must not contain a null value"
}

func (v noNullValuesValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noNullValuesValidator) ValidateMap(ctx context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for key, elem := range req.ConfigValue.Elements() {
		if elem.IsNull() {
			resp.Diagnostics.AddAttributeError(req.Path.AtMapKey(key), "Null Value", nullElementDetail)
		}
	}
}

// NoNullValues returns a validator rejecting null values in a map.
func NoNullValues() validator.Map {
	return noNullValuesValidator{}
}
