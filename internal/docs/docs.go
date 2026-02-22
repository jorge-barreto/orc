package docs

import "fmt"

// Topic holds a single documentation article.
type Topic struct {
	Name    string // short slug used as CLI argument
	Title   string // human-readable title
	Summary string // one-line description for topic listing
	Content string // full article text (plain text, no ANSI)
}

// All returns every topic in display order.
func All() []Topic {
	return topics
}

// Get looks up a topic by name. Returns an error with a hint if not found.
func Get(name string) (Topic, error) {
	for _, t := range topics {
		if t.Name == name {
			return t, nil
		}
	}
	return Topic{}, fmt.Errorf("unknown topic %q — run 'orc docs' to list available topics", name)
}
