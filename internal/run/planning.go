package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (e *Engine) generatePlanningBundle(ctx context.Context, r *state.Run, epic *state.Epic) (planningBundle, error) {
	if simulationMode() {
		return planningBundle{
			PRDMarkdown:           fmt.Sprintf("# %s\n\n## Problem\n\n%s\n\n## Requirements\n\n- Implement the requested behavior.\n- Preserve build, preview, and validation health.\n", epic.Title, epic.Body),
			ADRMarkdown:           fmt.Sprintf("# ADR: %s\n\n## Decision\n\nImplement the request as the smallest coherent repo change, keeping existing architecture unless a change is explicitly required.\n", epic.Title),
			DesignSpecMarkdown:    fmt.Sprintf("# Design Spec: %s\n\n## Implementation Shape\n\nMake the smallest cohesive change across the affected files. Keep tests and validation close to the changed behavior.\n", epic.Title),
			TestScenariosMarkdown: fmt.Sprintf("# Test Scenarios: %s\n\n1. Happy path works\n2. Required validation or error state works\n3. Existing build and preview remain green\n", epic.Title),
			Complexity:            "s",
			Rationale:             "simulation fallback",
		}, nil
	}
	prompt := fmt.Sprintf(`Create a lean planning bundle for this software epic.
Return only JSON matching this shape:
{
  "complexity": "xs|s|m|l|xl",
  "complexity_rationale": "one concise sentence",
  "prd_markdown": "# ...",
  "adr_markdown": "# ...",
  "design_spec_markdown": "# ...",
  "test_scenarios_markdown": "# ..."
}

Planning policy:
- These artifacts are for human inspection and durable documentation, not ceremony.
- Keep PRD under 600 words.
- Keep ADR under 400 words.
- Keep DesignSpec under 600 words.
- Keep TestScenarios to at most 5 numbered scenarios.
- For trivial/localized work, be very brief and concrete.
- Do not include implementation tickets here.
- Each markdown field must start with a level-one heading.

Complexity rubric:
- xs: copy/config/one localized UI or code change, normally one ticket.
- s: small localized feature or bug fix, normally one ticket.
- m: multi-file feature with some risk, normally 2-3 tickets.
- l: cross-module/system feature, normally 3-6 tickets.
- xl: migration/multiple services/high-risk work, may need more.

Epic title: %s
Epic body:
%s`, epic.Title, epic.Body)
	res, err := e.invokeRunner(ctx, r, "plan", prompt, "planner", "")
	if err != nil {
		return planningBundle{}, err
	}
	bundle, err := parsePlanningBundle(res.Output)
	if err != nil {
		return planningBundle{}, err
	}
	return bundle, nil
}

func parsePlanningBundle(raw string) (planningBundle, error) {
	raw = extractJSON(raw)
	var bundle planningBundle
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
		return planningBundle{}, fmt.Errorf("parse planning bundle JSON: %w", err)
	}
	bundle.Complexity = normalizeComplexity(bundle.Complexity)
	if bundle.Complexity == "" {
		return planningBundle{}, fmt.Errorf("planning bundle missing complexity")
	}
	checks := map[string]string{
		"prd_markdown":            bundle.PRDMarkdown,
		"adr_markdown":            bundle.ADRMarkdown,
		"design_spec_markdown":    bundle.DesignSpecMarkdown,
		"test_scenarios_markdown": bundle.TestScenariosMarkdown,
	}
	for name, body := range checks {
		if strings.TrimSpace(body) == "" || !strings.HasPrefix(strings.TrimSpace(body), "#") {
			return planningBundle{}, fmt.Errorf("planning bundle field %s must be non-empty markdown starting with #", name)
		}
	}
	return bundle, nil
}

func (e *Engine) generateArtifactBody(ctx context.Context, r *state.Run, phase, role, prompt string, fallback func() string) (string, error) {
	if simulationMode() {
		return fallback(), nil
	}
	res, err := e.invokeRunner(ctx, r, phase, prompt+"\n\nReturn only markdown. Start with a level-one heading.", role, "")
	if err != nil {
		return "", err
	}
	body := extractMarkdown(res.Output)
	if strings.TrimSpace(body) == "" || !strings.HasPrefix(strings.TrimSpace(body), "#") {
		return "", fmt.Errorf("%s runner returned empty or invalid markdown", role)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, phase, "artifact", "", "passed", map[string]any{"role": role, "model": res.Model})
	return body, nil
}

func extractMarkdown(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "```") {
		parts := strings.Split(s, "```")
		for i := 1; i < len(parts); i += 2 {
			block := strings.TrimSpace(parts[i])
			block = strings.TrimPrefix(block, "markdown")
			block = strings.TrimPrefix(block, "md")
			block = strings.TrimSpace(block)
			if strings.HasPrefix(block, "#") {
				return block
			}
		}
	}
	if idx := strings.Index(s, "# "); idx >= 0 {
		return strings.TrimSpace(s[idx:])
	}
	return s
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "```") {
		parts := strings.Split(s, "```")
		for i := 1; i < len(parts); i += 2 {
			block := strings.TrimSpace(parts[i])
			block = strings.TrimPrefix(block, "json")
			block = strings.TrimSpace(block)
			if strings.HasPrefix(block, "{") || strings.HasPrefix(block, "[") {
				return block
			}
		}
	}
	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")
	start := -1
	end := -1
	if startObj >= 0 && (startArr < 0 || startObj < startArr) {
		start = startObj
		end = strings.LastIndex(s, "}")
	} else if startArr >= 0 {
		start = startArr
		end = strings.LastIndex(s, "]")
	}
	if start >= 0 && end >= start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

func (e *Engine) planTickets(ctx context.Context, r *state.Run, epic *state.Epic, arts []state.Artifact) ([]plannedTicket, error) {
	if simulationMode() {
		plan := ticketPlan{Complexity: "s", Rationale: "simulation fallback", Tickets: defaultTicketPlan()}
		_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "ticketize", "ticket_plan", "", "passed", map[string]any{"tickets": len(plan.Tickets), "complexity": plan.Complexity, "rationale": plan.Rationale, "model": "stub"})
		return plan.Tickets, nil
	}
	var artifactSummary []string
	for _, a := range arts {
		artifactSummary = append(artifactSummary, fmt.Sprintf("%s: %s\n%s", a.Type, a.Title, truncate(a.Body, 1200)))
	}
	prompt := fmt.Sprintf(`Create an efficient dependency-aware ticket plan for this epic.
Return only JSON matching:
{
  "complexity": "xs|s|m|l|xl",
  "complexity_rationale": "one concise sentence",
  "tickets": [
    {
      "type": "feature|test|docs|bug",
      "title": "...",
      "body": "...",
      "acceptance_criteria": ["..."],
      "depends_on_titles": ["..."],
      "estimated_complexity": "xs|s|m|l|xl",
      "split_justification": ""
    }
  ]
}

Ticket policy:
- Bias hard toward larger and fewer tickets because the coding runner is capable.
- Default to exactly one ticket for xs and s work.
- Tests, docs, accessibility, preview checks, and validation are usually acceptance criteria inside the implementation ticket, not separate tickets.
- Split only for true dependency ordering, real parallelism, high-risk migrations, or independently reviewable cross-module work.
- If you split, every split ticket must include a concrete split_justification.
- Ticket count caps by complexity: xs=1, s=1, m=3, l=6, xl=12.
- A simple static-page or localized UI change should be one ticket.

Epic title: %s
Epic body:
%s

Artifacts:
%s`, epic.Title, epic.Body, strings.Join(artifactSummary, "\n\n---\n\n"))
	res, err := e.invokeRunner(ctx, r, "ticketize", prompt, "planner", e.runWorkdir(ctx, r))
	if err != nil {
		return nil, err
	}
	plan, err := parseTicketPlanEnvelope(res.Output)
	if err != nil {
		return nil, err
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "ticketize", "ticket_plan", "", "passed", map[string]any{"tickets": len(plan.Tickets), "complexity": plan.Complexity, "rationale": plan.Rationale, "model": res.Model})
	return plan.Tickets, nil
}

func parseTicketPlan(raw string) ([]plannedTicket, error) {
	plan, err := parseTicketPlanEnvelope(raw)
	if err != nil {
		return nil, err
	}
	return plan.Tickets, nil
}

func parseTicketPlanEnvelope(raw string) (ticketPlan, error) {
	raw = extractJSON(raw)
	var plan ticketPlan
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if err := json.Unmarshal([]byte(raw), &plan.Tickets); err != nil {
			return ticketPlan{}, fmt.Errorf("parse planner ticket JSON: %w", err)
		}
		plan.Complexity = inferComplexity(len(plan.Tickets))
		plan.Rationale = "inferred from legacy array output"
	} else if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return ticketPlan{}, fmt.Errorf("parse planner ticket JSON: %w", err)
	}
	plan.Complexity = normalizeComplexity(plan.Complexity)
	if plan.Complexity == "" {
		return ticketPlan{}, fmt.Errorf("planner ticket plan missing complexity")
	}
	specs := plan.Tickets
	if len(specs) == 0 {
		return ticketPlan{}, fmt.Errorf("planner returned no tickets")
	}
	if max := maxTicketsForComplexity(plan.Complexity); len(specs) > max {
		return ticketPlan{}, fmt.Errorf("planner returned %d tickets for complexity %s; max is %d", len(specs), plan.Complexity, max)
	}
	seen := map[string]bool{}
	for i := range specs {
		specs[i].Type = strings.TrimSpace(specs[i].Type)
		specs[i].Title = strings.TrimSpace(specs[i].Title)
		specs[i].Body = strings.TrimSpace(specs[i].Body)
		specs[i].EstimatedComplexity = normalizeComplexity(specs[i].EstimatedComplexity)
		specs[i].SplitJustification = strings.TrimSpace(specs[i].SplitJustification)
		if specs[i].Type == "" {
			specs[i].Type = "feature"
		}
		if specs[i].Title == "" || specs[i].Body == "" {
			return ticketPlan{}, fmt.Errorf("planner ticket missing title/body")
		}
		if len(specs[i].AcceptanceCriteria) == 0 {
			return ticketPlan{}, fmt.Errorf("planner ticket %q missing acceptance_criteria", specs[i].Title)
		}
		for j := range specs[i].AcceptanceCriteria {
			specs[i].AcceptanceCriteria[j] = strings.TrimSpace(specs[i].AcceptanceCriteria[j])
			if specs[i].AcceptanceCriteria[j] == "" {
				return ticketPlan{}, fmt.Errorf("planner ticket %q has empty acceptance criterion", specs[i].Title)
			}
		}
		if seen[specs[i].Title] {
			return ticketPlan{}, fmt.Errorf("duplicate planner ticket title: %s", specs[i].Title)
		}
		seen[specs[i].Title] = true
	}
	if len(specs) > 1 {
		for _, spec := range specs {
			if !strongSplitJustification(spec.SplitJustification) {
				return ticketPlan{}, fmt.Errorf("planner split ticket %q missing strong split_justification", spec.Title)
			}
		}
	}
	for _, spec := range specs {
		for _, dep := range spec.DependsOnTitles {
			if !seen[dep] {
				return ticketPlan{}, fmt.Errorf("planner ticket %q depends on unknown title %q", spec.Title, dep)
			}
		}
	}
	plan.Tickets = specs
	return plan, nil
}

func normalizeComplexity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "xs", "s", "m", "l", "xl":
		return s
	default:
		return ""
	}
}

func inferComplexity(n int) string {
	switch {
	case n <= 1:
		return "s"
	case n <= 3:
		return "m"
	case n <= 6:
		return "l"
	default:
		return "xl"
	}
}

func maxTicketsForComplexity(complexity string) int {
	switch complexity {
	case "xs", "s":
		return 1
	case "m":
		return 3
	case "l":
		return 6
	case "xl":
		return 12
	default:
		return 1
	}
}

func strongSplitJustification(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 40 {
		return false
	}
	for _, marker := range []string{"dependency", "parallel", "migration", "cross-module", "risk", "independent", "separate", "sequence", "integration"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func (e *Engine) createPlannedTickets(ctx context.Context, epicID, runID string, specs []plannedTicket) ([]*state.Ticket, error) {
	byTitle := map[string]*state.Ticket{}
	var out []*state.Ticket
	existing, _ := e.DB.ListTicketsForRun(ctx, epicID, runID)
	for i := range existing {
		byTitle[existing[i].Title] = &existing[i]
	}
	for _, spec := range specs {
		if existingTicket := byTitle[spec.Title]; existingTicket != nil {
			out = append(out, existingTicket)
			continue
		}
		body := spec.Body
		if len(spec.AcceptanceCriteria) > 0 {
			body += "\n\nAcceptance criteria:\n"
			for _, criterion := range spec.AcceptanceCriteria {
				body += "- " + criterion + "\n"
			}
		}
		if spec.EstimatedComplexity != "" {
			body += "\nEstimated complexity: " + spec.EstimatedComplexity + "\n"
		}
		if spec.SplitJustification != "" {
			body += "\nSplit justification: " + spec.SplitJustification + "\n"
		}
		t, err := e.DB.CreateTicketForRun(ctx, epicID, runID, spec.Type, spec.Title, body, nil)
		if err != nil {
			return nil, err
		}
		byTitle[spec.Title] = t
		out = append(out, t)
	}
	for _, spec := range specs {
		t := byTitle[spec.Title]
		for _, title := range spec.DependsOnTitles {
			dep := byTitle[title]
			if dep == nil {
				return nil, fmt.Errorf("missing dependency %q for %q", title, spec.Title)
			}
			if err := e.DB.AddDependency(ctx, t.ID, dep.ID); err != nil {
				return nil, err
			}
			t.DependsOn = append(t.DependsOn, dep.ID)
		}
	}
	return out, nil
}
