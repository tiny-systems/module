package tools

import (
	"fmt"
	"sort"
	"strings"
)

// The guide is read in full at the start of every session, before anyone
// knows what the flow will turn out to be. Roughly two fifths of it answers
// questions most flows never ask — how a dashboard widget renders, how a
// tool-using agent loop is wired, how to publish a solution — and that part
// was paid for whether or not it was ever relevant.
//
// So the guide splits: what every flow needs is returned as before, and the
// conditional parts are named and fetched when the flow turns out to need
// them. Naming them is the load-bearing half. A section that is silently
// absent invites improvisation, which is worse than paying for the text.

// deferredSections maps the argument a caller passes to the heading it
// selects. Matching is by heading prefix, so renaming a heading merely stops
// deferring that section — it stays in the core guide rather than
// disappearing, which is the right way for this to fail.
var deferredSections = []struct {
	key     string
	heading string
	needed  string
}{
	{"dashboards", "Dashboard Widgets", "the flow shows anything to a person"},
	{"forms", "Form Schemas", "you are authoring a form or control schema"},
	{"agents", "Building Agents", "the flow is a tool-using model loop"},
	{"code", "Code / Eval Components", "the flow runs js_eval or another code component"},
	{"scenarios", "Scenarios", "you are validating edges against sample data"},
	{"endpoints", "Verifying a Live Endpoint", "the flow serves HTTP and you need to prove it answers"},
	{"publishing", "Publishing a Solution", "you are publishing the project as a solution"},
}

// promptSection is one "## " block of the guide, kept with its heading so it
// can be handed back exactly as written.
type promptSection struct {
	heading string
	body    string
}

// splitPrompt cuts the guide at its top-level headings, returning whatever
// precedes the first one alongside the sections.
func splitPrompt(text string) (string, []promptSection) {
	lines := strings.Split(text, "\n")

	var (
		preamble []string
		sections []promptSection
		current  *promptSection
	)
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if current != nil {
				sections = append(sections, *current)
			}
			current = &promptSection{heading: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if current == nil {
			preamble = append(preamble, line)
			continue
		}
		current.body += line + "\n"
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return strings.Join(preamble, "\n"), sections
}

// deferralFor reports which argument key, if any, defers a heading.
func deferralFor(heading string) (string, bool) {
	for _, d := range deferredSections {
		if strings.HasPrefix(heading, d.heading) {
			return d.key, true
		}
	}
	return "", false
}

// corePrompt returns the guide without its conditional sections, followed by
// an index naming what was held back and when each becomes relevant.
func corePrompt(text string) string {
	preamble, sections := splitPrompt(text)

	var (
		b       strings.Builder
		held    = map[string]bool{}
		anyHeld bool
	)
	b.WriteString(preamble)
	for _, s := range sections {
		if key, deferred := deferralFor(s.heading); deferred {
			held[key] = true
			anyHeld = true
			continue
		}
		b.WriteString("\n## " + s.heading + "\n")
		b.WriteString(s.body)
	}
	if !anyHeld {
		return b.String()
	}

	b.WriteString("\n## Sections Not Included Above\n\n")
	b.WriteString("These answer questions most flows never ask, so they are fetched when they apply.\n")
	b.WriteString("Read the matching one BEFORE building that part — do not improvise it.\n\n")
	for _, d := range deferredSections {
		if !held[d.key] {
			continue
		}
		b.WriteString(fmt.Sprintf("- get_instructions(section: %q) — read when %s\n", d.key, d.needed))
	}
	return b.String()
}

// sectionPrompt returns one named section, or an error naming the ones that
// exist. Every deferred key is matched, so a caller asking for a section this
// deployment's guide does not contain is told so rather than handed nothing.
func sectionPrompt(text, key string) (string, error) {
	_, sections := splitPrompt(text)

	var b strings.Builder
	for _, s := range sections {
		if k, deferred := deferralFor(s.heading); deferred && k == key {
			b.WriteString("## " + s.heading + "\n")
			b.WriteString(s.body)
		}
	}
	if b.Len() > 0 {
		return strings.TrimRight(b.String(), "\n"), nil
	}

	known := make([]string, 0, len(deferredSections))
	for _, d := range deferredSections {
		known = append(known, d.key)
	}
	sort.Strings(known)
	return "", fmt.Errorf("unknown section %q; available: %s", key, strings.Join(known, ", "))
}
