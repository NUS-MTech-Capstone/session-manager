package events

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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

func TestSNSPublisherPublishAndEmit(t *testing.T) {
	received := make(chan string, 2)
	publisher := &SNSPublisher{
		topicARN: "arn:aws:sns:ap-southeast-1:123:events", region: "ap-southeast-1",
		creds: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "key", SecretAccessKey: "secret", Source: "test"}, nil
		}),
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			received <- string(body)
			if req.Header.Get("Authorization") == "" {
				t.Fatal("SNS request was not signed")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
		})},
	}
	if err := publisher.publish(context.Background(), "message"); err != nil {
		t.Fatalf("publish() error = %v", err)
	}
	<-received
	publisher.Emit(context.Background(), Event{EventType: "SessionCreated", SessionID: "sess-1"})
	select {
	case form := <-received:
		if !strings.Contains(form, "SessionCreated") || !strings.Contains(form, SchemaVersion) || !strings.Contains(form, "session-manager") {
			t.Fatalf("published form = %q", form)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for asynchronous publish")
	}
	publisher.Emit(context.Background(), Event{})

	publisher.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("bad")), Header: make(http.Header)}, nil
	})
	if err := publisher.publish(context.Background(), "message"); err == nil {
		t.Fatal("non-2xx response accepted")
	}
	publisher.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	if err := publisher.publish(context.Background(), "message"); err == nil {
		t.Fatal("transport error accepted")
	}
	publisher.creds = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) { return aws.Credentials{}, errors.New("no credentials") })
	if err := publisher.publish(context.Background(), "message"); err == nil {
		t.Fatal("credential error accepted")
	}
	if got := newID(); len(got) != 32 {
		t.Fatalf("newID() length = %d", len(got))
	}
}
