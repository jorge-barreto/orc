package docs

import "testing"

func TestAll_ReturnsTopics(t *testing.T) {
	topics := All()
	if len(topics) == 0 {
		t.Fatal("All() returned no topics")
	}
	if topics[0].Name != "quickstart" {
		t.Errorf("first topic = %q, want %q", topics[0].Name, "quickstart")
	}
}

func TestAll_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, topic := range All() {
		if seen[topic.Name] {
			t.Errorf("duplicate topic name: %q", topic.Name)
		}
		seen[topic.Name] = true
	}
}

func TestAll_AllFieldsPopulated(t *testing.T) {
	for _, topic := range All() {
		if topic.Name == "" {
			t.Error("topic has empty Name")
		}
		if topic.Title == "" {
			t.Errorf("topic %q has empty Title", topic.Name)
		}
		if topic.Summary == "" {
			t.Errorf("topic %q has empty Summary", topic.Name)
		}
		if topic.Content == "" {
			t.Errorf("topic %q has empty Content", topic.Name)
		}
	}
}

func TestGet_Found(t *testing.T) {
	topic, err := Get("quickstart")
	if err != nil {
		t.Fatalf("Get(quickstart) error: %v", err)
	}
	if topic.Name != "quickstart" {
		t.Errorf("Name = %q, want %q", topic.Name, "quickstart")
	}
}

func TestGet_NotFound(t *testing.T) {
	_, err := Get("nonexistent")
	if err == nil {
		t.Fatal("Get(nonexistent) should return error")
	}
}
