package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestMarksFormats(t *testing.T) {
	f, err := os.Open("../../../test_marks_formats.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	courses, err := Courses(f)
	if err != nil {
		t.Fatalf("ERROR: %v", err)
	}

	data, _ := json.MarshalIndent(courses, "", "  ")
	fmt.Println(string(data))

	// Expected results:
	// Course 1: 20.40[20.40+0.00]/40.00 -> Have: 20.40, Max: 40.00
	// Course 2: 50[49+1.00]/49 -> Have: 50, Max: 49
	// Course 3: 27.5[25.5+2.00]/40 -> Have: 27.5, Max: 40

	tests := []struct {
		code        string
		expectedHave float32
		expectedMax  float32
	}{
		{"CSE303", 20.40, 40.00},
		{"SKE301", 50.00, 49.00},
		{"BS309", 27.5, 40.00},
	}

	for i, tt := range tests {
		if i >= len(courses) {
			t.Errorf("Not enough courses parsed. Expected at least %d, got %d", i+1, len(courses))
			continue
		}

		course := courses[i]
		if course.Code != tt.code {
			t.Errorf("Course %d: expected code %s, got %s", i, tt.code, course.Code)
		}

		if course.InternalMarks.Have != tt.expectedHave {
			t.Errorf("Course %s: expected Have=%v, got %v", tt.code, tt.expectedHave, course.InternalMarks.Have)
		}

		if course.InternalMarks.Max != tt.expectedMax {
			t.Errorf("Course %s: expected Max=%v, got %v", tt.code, tt.expectedMax, course.InternalMarks.Max)
		}
	}
}
