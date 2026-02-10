package parse

import (
	"os"
	"testing"
)

// TestCoursesWithRealData tests parsing with actual HTML from Amizone HAR file
func TestCoursesWithRealData(t *testing.T) {
	harFile := "../../../har_extracted_Academics_MyCourses_80.html"

	file, err := os.Open(harFile)
	if err != nil {
		t.Skipf("Skipping test - HAR file not found: %v", err)
		return
	}
	defer file.Close()

	courses, err := Courses(file)
	if err != nil {
		t.Fatalf("Failed to parse real courses data: %v", err)
	}

	if len(courses) == 0 {
		t.Fatal("Expected courses but got none")
	}

	t.Logf("Successfully parsed %d courses", len(courses))

	// Verify we have courses with attendance
	coursesWithAttendance := 0
	coursesWithNA := 0
	coursesWithMarks := 0

	for i, course := range courses {
		t.Logf("Course %d: %s (%s)", i+1, course.Name, course.Code)

		if course.Attendance.ClassesHeld > 0 {
			coursesWithAttendance++
			t.Logf("  Attendance: %d/%d", course.Attendance.ClassesAttended, course.Attendance.ClassesHeld)
		} else {
			coursesWithNA++
			t.Logf("  Attendance: N/A")
		}

		if course.InternalMarks.Max > 0 {
			coursesWithMarks++
			t.Logf("  Marks: %.2f/%.2f", course.InternalMarks.Have, course.InternalMarks.Max)
		} else {
			t.Logf("  Marks: Not published")
		}

		// Verify required fields are present
		if course.Code == "" {
			t.Errorf("Course %d has no code", i+1)
		}
		if course.Name == "" {
			t.Errorf("Course %d has no name", i+1)
		}
	}

	t.Logf("Courses with attendance: %d", coursesWithAttendance)
	t.Logf("Courses with N/A attendance: %d", coursesWithNA)
	t.Logf("Courses with marks: %d", coursesWithMarks)

	// We expect at least 9 courses with attendance (based on our analysis)
	if coursesWithAttendance < 9 {
		t.Errorf("Expected at least 9 courses with attendance, got %d", coursesWithAttendance)
	}

	// We expect exactly 1 course with N/A attendance (based on our analysis)
	if coursesWithNA != 1 {
		t.Errorf("Expected 1 course with N/A attendance, got %d", coursesWithNA)
	}

	// All marks should be empty for this dataset
	if coursesWithMarks != 0 {
		t.Errorf("Expected 0 courses with marks (not yet published), got %d", coursesWithMarks)
	}
}

// TestProfileWithNotApplicable tests profile parsing with "Not Applicable" response
func TestProfileWithNotApplicable(t *testing.T) {
	harFile := "../../../har_extracted_IDCard_14.html"

	file, err := os.Open(harFile)
	if err != nil {
		t.Skipf("Skipping test - HAR file not found: %v", err)
		return
	}
	defer file.Close()

	profile, err := Profile(file)

	if err != nil {
		t.Fatalf("Expected no error for 'Not Applicable' page, got: %v", err)
	}
	if profile == nil {
		t.Fatalf("Expected a profile object, got nil")
	}
	if profile.Name != "" || profile.EnrollmentNumber != "" || profile.Program != "" || profile.Batch != "" {
		t.Fatalf("Expected empty profile for 'Not Applicable' response, got: %+v", profile)
	}
}
