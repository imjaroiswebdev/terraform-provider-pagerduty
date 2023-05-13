package pagerduty

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/heimweh/go-pagerduty/pagerduty"
)

var (
	routerPathForRuleUpdateMutex sync.Mutex
	routerPathForRuleUpdate      *pagerduty.EventOrchestrationPath
)

func resourcePagerDutyEventOrchestrationPathRouterRule() *schema.Resource {
	return &schema.Resource{
		ReadContext:   resourcePagerDutyEventOrchestrationPathRouterRuleRead,
		CreateContext: resourcePagerDutyEventOrchestrationPathRouterRuleCreate,
		UpdateContext: resourcePagerDutyEventOrchestrationPathRouterRuleUpdate,
		DeleteContext: resourcePagerDutyEventOrchestrationPathRouterRuleDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourcePagerDutyEventOrchestrationPathRouterRuleImport,
		},
		Schema: map[string]*schema.Schema{
			"event_orchestration": {
				Type:     schema.TypeString,
				Required: true,
			},
			"label": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"condition": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: eventOrchestrationPathConditionsSchema,
				},
			},
			"actions": {
				Type:     schema.TypeList,
				Required: true,
				MaxItems: 1, //there can only be one action for router
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"route_to": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: func(v interface{}, key string) (warns []string, errs []error) {
								value := v.(string)
								if value == "unrouted" {
									errs = append(errs, fmt.Errorf("route_to within a set's rule has to be a Service ID. Got: %q", v))
								}
								return
							},
						},
					},
				},
			},
			"disabled": {
				Type:     schema.TypeBool,
				Optional: true,
			},
		},
	}
}

func resourcePagerDutyEventOrchestrationPathRouterRuleRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return fetchEventOrchestrationPathRouterRule(ctx, d, meta)
}

func fetchEventOrchestrationPathRouterRule(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	client, err := meta.(*Config).Client()
	if err != nil {
		return diag.FromErr(err)
	}

	retryErr := resource.RetryContext(ctx, 2*time.Minute, func() *resource.RetryError {
		log.Printf("[INFO] Reading PagerDuty Event Orchestration Path of type %s for orchestration: %s", "router", d.Id())

		eoId := d.Get("event_orchestration").(string)
		if routerPath, _, err := client.EventOrchestrationPaths.GetContext(ctx, eoId, "router"); err != nil {
			time.Sleep(2 * time.Second)
			return resource.RetryableError(err)
		} else if routerPath != nil {

			if routerPath.Sets != nil {
				routerRules := routerPath.Sets[0].Rules // Only using id = "start" Set

				var wasFound bool
				for _, rule := range routerRules {
					if rule.ID == d.Id() {
						setEventOrchestrationRouterRuleState(d, rule)
						wasFound = true
						break
					}
				}
				if !wasFound {
					err = fmt.Errorf("event orchestration path router rule %s not found", d.Id())
					return resource.RetryableError(err)
				}
			}
		}
		return nil
	})

	if retryErr != nil {
		return diag.FromErr(retryErr)
	}

	return diags
}

func getEventOrchestrationPathRouter(ctx context.Context, d *schema.ResourceData, meta interface{}) (*pagerduty.EventOrchestrationPath, error) {
	client, err := meta.(*Config).Client()
	if err != nil {
		return nil, err
	}

	var routerPath *pagerduty.EventOrchestrationPath
	retryErr := resource.RetryContext(ctx, 2*time.Minute, func() *resource.RetryError {
		eoId := d.Get("event_orchestration").(string)
		var err error
		if routerPath, _, err = client.EventOrchestrationPaths.GetContext(ctx, eoId, "router"); err != nil && routerPath != nil {
			time.Sleep(2 * time.Second)
			return resource.RetryableError(err)
		}
		return nil
	})

	if retryErr != nil {
		return nil, retryErr
	}

	return routerPath, nil
}

// EventOrchestrationPath cannot be created, use update to add / edit / remove rules and sets
func resourcePagerDutyEventOrchestrationPathRouterRuleCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	return resourcePagerDutyEventOrchestrationPathRouterRuleUpdate(ctx, d, meta)
}

// TODO: Implement this. Just boilerplate code
func resourcePagerDutyEventOrchestrationPathRouterRuleDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, err := meta.(*Config).Client()
	if err != nil {
		return diag.FromErr(err)
	}

	// In order to delete an Orchestration Router an empty orchestration path
	// config should be sent as an update.
	emptyPath := emptyOrchestrationPathStructBuilder("router")
	routerID := d.Get("event_orchestration").(string)

	log.Printf("[INFO] Deleting PagerDuty Event Orchestration Router Path: %s", routerID)

	retryErr := resource.RetryContext(ctx, 30*time.Second, func() *resource.RetryError {
		if _, _, err := client.EventOrchestrationPaths.UpdateContext(ctx, routerID, "router", emptyPath); err != nil {
			return resource.RetryableError(err)
		}
		return nil
	})

	if retryErr != nil {
		return diag.FromErr(retryErr)
	}

	d.SetId("")
	return nil
}

func resourcePagerDutyEventOrchestrationPathRouterRuleUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	client, err := meta.(*Config).Client()
	if err != nil {
		return diag.FromErr(err)
	}

	// Lock the mutex to ensure only one instance is created or updated at a time
	routerPathForRuleUpdateMutex.Lock()
	defer routerPathForRuleUpdateMutex.Unlock()

	routerPathForRuleUpdate, err = getEventOrchestrationPathRouter(ctx, d, meta)
	if err != nil {
		return diag.FromErr(err)
	}

	ruleUpdate := &pagerduty.EventOrchestrationPathRule{
		Label:      d.Get("label").(string),
		Disabled:   d.Get("disabled").(bool),
		Conditions: expandEventOrchestrationPathConditions(d.Get("condition")),
		Actions:    expandRouterActions(d.Get("actions")),
	}
	var updateAtIndex int
	updatedRouterPathForRuleUpdate, updateAtIndex := addRuleToRouterPathStructForUpdate(d.Id(), ruleUpdate, routerPathForRuleUpdate)

	// routerPath := buildRouterPathStructForUpdate(d)
	var warnings []*pagerduty.EventOrchestrationPathWarning

	log.Printf("[INFO] Updating PagerDuty Event Orchestration Path of type %s for orchestration: %s", "router", updatedRouterPathForRuleUpdate.Parent.ID)

	retryErr := resource.RetryContext(ctx, 30*time.Second, func() *resource.RetryError {
		response, _, err := client.EventOrchestrationPaths.UpdateContext(ctx, updatedRouterPathForRuleUpdate.Parent.ID, "router", updatedRouterPathForRuleUpdate)
		if err != nil {
			return resource.RetryableError(err)
		}
		if response == nil {
			return resource.NonRetryableError(fmt.Errorf("No Event Orchestration Router found."))
		}
		d.SetId(response.OrchestrationPath.Sets[0].Rules[updateAtIndex].ID)
		d.Set("event_orchestration", updatedRouterPathForRuleUpdate.Parent.ID)
		warnings = response.Warnings

		setEventOrchestrationRouterRuleState(d, ruleUpdate)
		return nil
	})

	if retryErr != nil {
		time.Sleep(2 * time.Second)
		return diag.FromErr(retryErr)
	}

	return convertEventOrchestrationPathWarningsToDiagnostics(warnings, diags)
}

func resourcePagerDutyEventOrchestrationPathRouterRuleImport(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	client, err := meta.(*Config).Client()
	if err != nil {
		return []*schema.ResourceData{}, err
	}
	// given an orchestration ID import the router orchestration path
	orchestrationID := d.Id()
	pathType := "router"
	_, _, err = client.EventOrchestrationPaths.GetContext(ctx, orchestrationID, pathType)

	if err != nil {
		return []*schema.ResourceData{}, err
	}

	d.SetId(orchestrationID)
	d.Set("event_orchestration", orchestrationID)

	return []*schema.ResourceData{d}, nil
}

func setEventOrchestrationRouterRuleState(d *schema.ResourceData, rule *pagerduty.EventOrchestrationPathRule) {
	d.Set("label", rule.Label)
	d.Set("disabled", rule.Disabled)
	d.Set("condition", flattenEventOrchestrationPathConditions(rule.Conditions))
	d.Set("actions", flattenRouterActions(rule.Actions))
}

func addRuleToRouterPathStructForUpdate(ruleId string, rule *pagerduty.EventOrchestrationPathRule, routerPath *pagerduty.EventOrchestrationPath) (*pagerduty.EventOrchestrationPath, int) {
	var updateAtIndex int
	var hasToUpdate bool
	for i, rpRule := range routerPath.Sets[0].Rules {
		if rpRule.ID == ruleId {
			updateAtIndex = i
			hasToUpdate = true
			break
		}
	}

	if !hasToUpdate {
		routerPath.Sets[0].Rules = append(routerPath.Sets[0].Rules, rule)
		updateAtIndex = len(routerPath.Sets[0].Rules) - 1
		return routerPath, updateAtIndex
	}

	routerPath.Sets[0].Rules[updateAtIndex] = rule
	return routerPath, updateAtIndex
}
