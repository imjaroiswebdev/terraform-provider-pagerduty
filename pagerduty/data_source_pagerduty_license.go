package pagerduty

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/heimweh/go-pagerduty/pagerduty"
)

func dataSourcePagerDutyLicense() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourcePagerDutyLicenseRead,
		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"description": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"current_value": {
				Type:     schema.TypeInt,
				Computed: true,
			},
			"allocations_available": {
				Type:     schema.TypeInt,
				Computed: true,
			},
			"valid_roles": {
				Type:     schema.TypeList,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"role_group": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func dataSourcePagerDutyLicenseRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, err := meta.(*Config).Client()
	if err != nil {
		return diag.FromErr(err)
	}

	log.Printf("[INFO] Reading PagerDuty License data source")

	searchName := d.Get("name").(string)

	err = resource.RetryContext(ctx, 5*time.Minute, func() *resource.RetryError {
		resp, _, err := client.Licenses.ListContext(ctx)
		if err != nil {
			// Delaying retry by 30s as recommended by PagerDuty
			// https://developer.pagerduty.com/docs/rest-api-v2/rate-limiting/#what-are-possible-workarounds-to-the-events-api-rate-limit
			time.Sleep(30 * time.Second)
			return resource.RetryableError(err)
		}

		var found *pagerduty.License

		for _, license := range resp.Licenses {
			if license.Name == searchName {
				found = license
				break
			}
		}

		if found == nil {
			return resource.NonRetryableError(
				fmt.Errorf("unable to locate any license with name: %s", searchName),
			)
		}

		err = flattenLicense(d, found)
		if err != nil {
			return resource.NonRetryableError(err)
		}

		return nil
	})

	if err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func flattenLicense(d *schema.ResourceData, license *pagerduty.License) error {
	d.SetId(license.ID)
	d.Set("name", license.Name)
	if license.Description != "" {
		d.Set("description", license.Description)
	}
	d.Set("current_value", license.CurrentValue)
	d.Set("allocations_available", license.AllocationsAvailable)
	d.Set("role_group", license.RoleGroup)
	if license.ValidRoles != nil {
		d.Set("valid_roles", license.ValidRoles)
	}
	return nil
}
