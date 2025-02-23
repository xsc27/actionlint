package actionlint

import (
	"time"

	"github.com/robfig/cron"
)

//go:generate go run ./scripts/generate-webhook-events ./all_webhooks.go

// RuleEvents is a rule to check 'on' field in workflow.
// https://docs.github.com/en/actions/reference/events-that-trigger-workflows
type RuleEvents struct {
	RuleBase
}

// NewRuleEvents creates new RuleEvents instance.
func NewRuleEvents() *RuleEvents {
	return &RuleEvents{
		RuleBase: RuleBase{name: "events"},
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleEvents) VisitWorkflowPre(n *Workflow) error {
	for _, e := range n.On {
		rule.checkEvent(e)
	}
	return nil
}

func (rule *RuleEvents) checkEvent(event Event) {
	switch e := event.(type) {
	case *ScheduledEvent:
		for _, c := range e.Cron {
			rule.checkCron(c)
		}
	case *WorkflowDispatchEvent:
		// Nothing to do
	case *RepositoryDispatchEvent:
		// Nothing to do
	case *WebhookEvent:
		rule.checkWebhookEvent(e)
	default:
		panic("unreachable")
	}
}

// https://docs.github.com/en/actions/reference/workflow-syntax-for-github-actions#onschedule
func (rule *RuleEvents) checkCron(spec *String) {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := p.Parse(spec.Value)
	if err != nil {
		rule.errorf(spec.Pos, "invalid CRON format %q in schedule event: %s", spec.Value, err.Error())
		return
	}

	start := sched.Next(time.Unix(0, 0))
	next := sched.Next(start)
	diff := next.Sub(start).Seconds()

	// (#14) https://docs.github.com/en/actions/reference/events-that-trigger-workflows#scheduled-events
	//
	// > The shortest interval you can run scheduled workflows is once every 5 minutes.
	if diff < 60.0*5 {
		rule.errorf(spec.Pos, "scheduled job runs too frequently. it runs once per %g seconds. the shortest interval is once every 5 minutes", diff)
	}
}

// https://docs.github.com/en/actions/reference/events-that-trigger-workflows#webhook-events
func (rule *RuleEvents) checkWebhookEvent(event *WebhookEvent) {
	hook := event.Hook.Value

	types, ok := AllWebhookTypes[hook]
	if !ok {
		rule.errorf(event.Pos, "unknown Webhook event %q. see https://docs.github.com/en/actions/reference/events-that-trigger-workflows#webhook-events for list of all Webhook event names", hook)
		return
	}

	rule.checkTypes(event.Hook, event.Types, types)

	if hook == "workflow_run" {
		if len(event.Workflows) == 0 {
			rule.error(event.Pos, "no workflow is configured for \"workflow_run\" event")
		}
	} else {
		if len(event.Workflows) != 0 {
			rule.errorf(event.Pos, "\"workflows\" cannot be configured for %q event. it is only for workflow_run event", hook)
		}
	}
}

func (rule *RuleEvents) checkTypes(hook *String, types []*String, expected []string) {
	if len(expected) == 0 && len(types) > 0 {
		rule.errorf(hook.Pos, "\"types\" cannot be specified for %q Webhook event", hook.Value)
		return
	}

	for _, ty := range types {
		valid := false
		for _, e := range expected {
			if ty.Value == e {
				valid = true
				break
			}
		}
		if !valid {
			rule.errorf(
				ty.Pos,
				"invalid activity type %q for %q Webhook event. available types are %s",
				ty.Value,
				hook.Value,
				sortedQuotes(expected),
			)
		}
	}
}
