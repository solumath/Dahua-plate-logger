package logger

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// eventStream produces a realistic Dahua multipart chunk matching the actual
// ITC237 wire format (index=0 between action and data, pretty-printed JSON).
func eventStream(plate string, utc, utcms int64) string {
	return fmt.Sprintf(
		"--myboundary\nContent-Type: text/plain\nContent-Length:0\n\n"+
			"Code=TrafficJunction;action=Pulse;index=0;data={\n"+
			"   \"Object\": {\"ObjectID\": 1, \"ObjectType\": \"Plate\", \"Text\": %q},\n"+
			"   \"TrafficCar\": {\"PlateNumber\": %q},\n"+
			"   \"UTC\": %d,\n"+
			"   \"UTCMS\": %d\n"+
			"}\n\n",
		plate, plate, utc, utcms,
	)
}

// tcOnlyStream produces an event where only TrafficCar has the plate (Object is a Vehicle).
func tcOnlyStream(plate string) string {
	return fmt.Sprintf(
		"--myboundary\nContent-Type: text/plain\n\n"+
			"Code=TrafficJunction;action=Pulse;index=0;data={\n"+
			"   \"Object\": {\"ObjectID\": 2, \"ObjectType\": \"Vehicle\", \"Text\": \"Toyota\"},\n"+
			"   \"TrafficCar\": {\"PlateNumber\": %q},\n"+
			"   \"UTC\": 1775733664,\n"+
			"   \"UTCMS\": 0\n"+
			"}\n\n",
		plate,
	)
}

func collectEvents(stream string) []Event {
	var events []Event
	_ = iterLines(strings.NewReader(stream), "cam", func(e Event) {
		events = append(events, e)
	})
	return events
}

// --- iterLines / end-to-end stream parsing ---

func TestIterLines_RealFormat(t *testing.T) {
	// Plate 9P08278 from an actual ITC237 capture; UTC 1775733664 UTCMS 232.
	events := collectEvents(eventStream("9P08278", 1775733664, 232))
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Plate != "9P08278" {
		t.Errorf("plate: got %q, want 9P08278", e.Plate)
	}
	if e.UTC != 1775733664 || e.UTCMS != 232 {
		t.Errorf("camera fields: got UTC=%d UTCMS=%d, want 1775733664 232", e.UTC, e.UTCMS)
	}
	if since := time.Since(e.Time()); since < 0 || since > time.Second {
		t.Errorf("ReceivedAt not close to now: %v", e.Time())
	}
}

func TestIterLines_MultipleEvents(t *testing.T) {
	stream := eventStream("9P08278", 1775733664, 232) +
		eventStream("1AB2345", 1775733700, 0)
	events := collectEvents(stream)
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].Plate != "9P08278" || events[1].Plate != "1AB2345" {
		t.Errorf("plates: got %q %q", events[0].Plate, events[1].Plate)
	}
}

func TestIterLines_ObjectTakesPriorityOverTrafficCar(t *testing.T) {
	// When Object.ObjectType == "Plate", Object.Text wins over TrafficCar.PlateNumber.
	stream := fmt.Sprintf(
		"Code=TrafficJunction;action=Pulse;index=0;data={\n" +
			"   \"Object\": {\"ObjectID\": 1, \"ObjectType\": \"Plate\", \"Text\": \"OBJECT1\"},\n" +
			"   \"TrafficCar\": {\"PlateNumber\": \"TRAFFIC\"},\n" +
			"   \"UTC\": 1700000000, \"UTCMS\": 0\n}\n",
	)
	events := collectEvents(stream)
	if len(events) != 1 || events[0].Plate != "OBJECT1" {
		t.Fatalf("got %v", events)
	}
}

func TestIterLines_FallsBackToTrafficCar(t *testing.T) {
	events := collectEvents(tcOnlyStream("4AB1234"))
	if len(events) != 1 || events[0].Plate != "4AB1234" {
		t.Fatalf("got %v", events)
	}
}

func TestIterLines_NoPlateSkipped(t *testing.T) {
	stream := "Code=TrafficJunction;action=Pulse;index=0;data={\n" +
		"   \"Object\": {\"ObjectID\": 1, \"ObjectType\": \"Vehicle\", \"Text\": \"\"},\n" +
		"   \"TrafficCar\": {\"PlateNumber\": \"\"},\n" +
		"   \"UTC\": 1700000000, \"UTCMS\": 0\n}\n"
	if events := collectEvents(stream); len(events) != 0 {
		t.Fatalf("want 0 events, got %d", len(events))
	}
}

func TestIterLines_NonTrafficLinesIgnored(t *testing.T) {
	stream := "--myboundary\nContent-Type: text/plain\n\nCode=Heartbeat;action=Pulse\n"
	if events := collectEvents(stream); len(events) != 0 {
		t.Fatalf("want 0 events, got %d", len(events))
	}
}

// --- braceDepthDelta ---

func TestBraceDepthDelta_BracesInsideStrings(t *testing.T) {
	// A real plate value like "{ABC}" inside a JSON string must not affect depth.
	if d := braceDepthDelta(`"Text": "{9P08278}"`); d != 0 {
		t.Fatalf("want 0, got %d", d)
	}
}

func TestBraceDepthDelta_EscapedQuote(t *testing.T) {
	// Escaped quote must not end the string prematurely.
	if d := braceDepthDelta(`"v": "he said \"{\"", {`); d != 1 {
		t.Fatalf("want 1, got %d", d)
	}
}

// --- isValidPlate ---

func TestIsValidPlate(t *testing.T) {
	valid := []string{"9P08278", "1AB2345", "ABCD1234", "A1"}
	for _, p := range valid {
		if !isValidPlate(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	invalid := []string{"", "TOOLONGPLATE", "abc123", "9P-0827", "123456789"}
	for _, p := range invalid {
		if isValidPlate(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
}
