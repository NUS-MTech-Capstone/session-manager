package events

import (
	"context"
	"testing"
)

func TestNewPublisherValidation(t *testing.T) {
	publisher, err := NewPublisher(context.Background(), false, "", "")
	if err != nil {
		t.Fatalf("disabled NewPublisher() error = %v", err)
	}
	if _, ok := publisher.(NoopPublisher); !ok {
		t.Fatalf("disabled publisher type = %T", publisher)
	}
	publisher.Emit(context.Background(), Event{})
	if _, err := NewPublisher(context.Background(), true, "region", ""); err == nil {
		t.Fatal("missing topic ARN accepted")
	}
	if _, err := NewPublisher(context.Background(), true, "", "topic"); err == nil {
		t.Fatal("missing region accepted")
	}
}
