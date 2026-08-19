package tools

import (
	"strings"
	"testing"
)

func TestCorePrompt_HoldsBackConditionalSectionsAndNamesThem(t *testing.T) {
	core := corePrompt(CorePrompt)

	if len(core) >= len(CorePrompt) {
		t.Fatalf("core is %d chars against %d — nothing was held back", len(core), len(CorePrompt))
	}
	// The load-bearing half: a section that vanishes silently invites the
	// agent to improvise the thing it was meant to explain.
	for _, key := range []string{"dashboards", "forms", "agents", "code", "scenarios", "endpoints", "publishing"} {
		if !strings.Contains(core, `section: "`+key+`"`) {
			t.Fatalf("core guide never names section %q", key)
		}
	}
}

// What every flow needs must stay in the core guide, whatever else moves.
func TestCorePrompt_KeepsWhatEveryFlowNeeds(t *testing.T) {
	core := corePrompt(CorePrompt)
	for _, heading := range []string{
		"## Core Concepts",
		"## How to Build a Flow",
		"## Expression Syntax",
		"## Credentials",
		"## System Ports",
		"## Leave No Port Dangling",
		"## Behavioral Rules",
	} {
		if !strings.Contains(core, heading) {
			t.Fatalf("core guide lost %q", heading)
		}
	}
}

func TestCorePrompt_DropsTheDeferredBodies(t *testing.T) {
	core := corePrompt(CorePrompt)
	for _, heading := range []string{"## Dashboard Widgets", "## Building Agents", "## Publishing a Solution"} {
		if strings.Contains(core, heading) {
			t.Fatalf("core guide still carries %q", heading)
		}
	}
}

func TestSectionPrompt_ReturnsTheSectionVerbatim(t *testing.T) {
	body, err := sectionPrompt(CorePrompt, "dashboards")
	if err != nil {
		t.Fatalf("dashboards: %v", err)
	}
	if !strings.HasPrefix(body, "## Dashboard Widgets") {
		t.Fatalf("got %.60q", body)
	}
	// The text handed back has to be the guide's own, not a paraphrase.
	if !strings.Contains(CorePrompt, strings.TrimPrefix(body, "## ")[:200]) {
		t.Fatal("section body does not match the guide")
	}
}

func TestSectionPrompt_EverySectionResolves(t *testing.T) {
	for _, d := range deferredSections {
		body, err := sectionPrompt(CorePrompt, d.key)
		if err != nil {
			t.Fatalf("%s: %v", d.key, err)
		}
		if len(strings.TrimSpace(body)) == 0 {
			t.Fatalf("%s resolved to nothing", d.key)
		}
	}
}

func TestSectionPrompt_UnknownKeyNamesTheRealOnes(t *testing.T) {
	_, err := sectionPrompt(CorePrompt, "widgets")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "dashboards") {
		t.Fatalf("error should list the available keys, got %v", err)
	}
}

// A host appends its own sections to the guide. Splitting must not depend on
// anything only the SDK's own text has.
func TestSplitPrompt_HandlesAppendedHostSections(t *testing.T) {
	appended := CorePrompt + "\n## Local MCP Server Context\n\nSomething a host added.\n"
	core := corePrompt(appended)
	if !strings.Contains(core, "Something a host added.") {
		t.Fatal("an appended section was lost from the core guide")
	}
}

// Renaming a heading should stop deferring it, not delete it.
func TestCorePrompt_UnknownHeadingsStayInCore(t *testing.T) {
	core := corePrompt("## Some Heading Nobody Deferred\n\nbody text\n")
	if !strings.Contains(core, "body text") {
		t.Fatal("an undeferred section was dropped")
	}
}

// A cross-reference like "see Dashboard Widgets below" is correct in the full
// guide and a dead end in the core one, where that section is no longer
// below. This is how the split rots: the text drifts, and nothing complains.
func TestCorePrompt_HasNoReferencesToHeldBackSections(t *testing.T) {
	core := corePrompt(CorePrompt)
	for _, d := range deferredSections {
		for _, phrasing := range []string{
			"see " + d.heading,
			d.heading + " below",
			d.heading + " above",
		} {
			if strings.Contains(core, phrasing) {
				t.Errorf("core guide says %q but that section is not in it — point at "+
					"get_instructions(section: %q) instead", phrasing, d.key)
			}
		}
	}
}
