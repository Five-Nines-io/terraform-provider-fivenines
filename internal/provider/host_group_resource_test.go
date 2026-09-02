package provider_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCheckHostGroupDestroyed() resource.TestCheckFunc {
	return checkDestroyed("fivenines_host_group", func(ctx context.Context, id string) error {
		numericID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return err
		}
		_, _, err = testAccClient().GetHostGroup(ctx, numericID)
		return err
	})
}

func testAccHostGroupConfig(name string) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_host_group" "test" {
  name = %[1]q
}
`, name)
}

func testAccHostGroupConfigWithPosition(name string, position int) string {
	return fmt.Sprintf(providerConfig+`
resource "fivenines_host_group" "test" {
  name     = %[1]q
  position = %[2]d
}
`, name, position)
}

// Full CRUD plus import. A group with no position takes whatever the API
// assigns, which is what makes position Optional+Computed rather than Optional.
//
// Note the group is created empty and stays empty: the API deletes a group once
// its LAST instance leaves, which is a transition an empty group never makes, so
// nothing disappears underneath the test.
func TestAccHostGroup_lifecycle(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-group")
	renamed := name + "-renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckHostGroupDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccHostGroupConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_host_group.test", "name", name),
					resource.TestCheckResourceAttrSet("fivenines_host_group.test", "id"),
					// Assigned by the API because the configuration does not pin it.
					resource.TestCheckResourceAttrSet("fivenines_host_group.test", "position"),
					resource.TestCheckResourceAttrSet("fivenines_host_group.test", "created_at"),
				),
			},
			{
				Config: testAccHostGroupConfigWithPosition(renamed, 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_host_group.test", "name", renamed),
					resource.TestCheckResourceAttr("fivenines_host_group.test", "position", "1"),
				),
			},
			{
				ResourceName:      "fivenines_host_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// A pinned position must not drift on a re-plan. This is the acceptance-level
// counterpart to the unit tests around unknownOnPositionChange: if the plan
// modifier or the config-vs-plan read regressed, the second apply of an
// identical configuration would show a diff instead of an empty plan.
func TestAccHostGroup_pinnedPositionIsStable(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-group-pos")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckHostGroupDestroyed(),
		Steps: []resource.TestStep{
			{
				Config: testAccHostGroupConfigWithPosition(name, 1),
				Check: resource.TestCheckResourceAttr(
					"fivenines_host_group.test", "position", "1"),
			},
			{
				Config:   testAccHostGroupConfigWithPosition(name, 1),
				PlanOnly: true,
			},
		},
	})
}
