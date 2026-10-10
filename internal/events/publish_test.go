package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testPublisher(transport testTransport) *SNSPublisher {
	return &SNSPublisher{topicARN: "arn:aws:sns:ap-southeast-1:123456789012:test-events", region: "ap-southeast-1",
		creds: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test-access", SecretAccessKey: "test-secret"}, nil
		}),
		client: &http.Client{Transport: transport}}
}

func TestPublishSignedRequestAndFailures(t *testing.T) {
	sentinel := errors.New("transport unavailable")
	for _, tc := range []struct {
		name         string
		status       int
		transportErr error
		wantErr      string
	}{
		{"success", 200, nil, ""}, {"rejected", 403, nil, "status 403"}, {"unavailable", 503, nil, "status 503"}, {"transport", 0, sentinel, "transport unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := testPublisher(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" || r.URL.Host != "sns.ap-southeast-1.amazonaws.com" {
					t.Errorf("unexpected destination: %s %s", r.Method, r.URL)
				}
				if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Error("request is not signed")
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("Action") != "Publish" || r.Form.Get("Version") != "2010-03-31" || r.Form.Get("Message") != "metadata" || !strings.HasSuffix(r.Form.Get("TopicArn"), ":test-events") {
					t.Errorf("incorrect publish form: %v", r.Form)
				}
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})
			err := p.publish(t.Context(), "metadata")
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPublishCredentialFailureDoesNotSend(t *testing.T) {
	denied := errors.New("credentials unavailable")
	p := testPublisher(func(*http.Request) (*http.Response, error) {
		t.Error("sent without credentials")
		return nil, errors.New("unexpected request")
	})
	p.creds = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) { return aws.Credentials{}, denied })
	if err := p.publish(t.Context(), "metadata"); !errors.Is(err, denied) {
		t.Fatalf("error=%v", err)
	}
}

func TestEmitCompletesAfterCallerCancellation(t *testing.T) {
	type captured struct {
		form     url.Values
		ctxErr   error
		deadline bool
	}
	requests := make(chan captured, 1)
	p := testPublisher(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		_, hasDeadline := r.Context().Deadline()
		requests <- captured{r.Form, r.Context().Err(), hasDeadline}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p.Emit(ctx, Event{EventType: "AccessAllowed", UserID: "user-1", SessionID: "session-1"})
	select {
	case got := <-requests:
		if got.ctxErr != nil || !got.deadline {
			t.Fatalf("publication must have its own bounded context: %+v", got)
		}
		var e Event
		if err := json.Unmarshal([]byte(got.form.Get("Message")), &e); err != nil {
			t.Fatal(err)
		}
		if e.SchemaVersion != SchemaVersion || e.EventID == "" || e.Source == "" || e.OccurredAt.IsZero() || e.EventType != "AccessAllowed" || e.UserID != "user-1" || e.SessionID != "session-1" {
			t.Fatalf("metadata not preserved/defaulted: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event not published")
	}
}

func TestEmptyEventIsRejectedBeforePublish(t *testing.T) {
	p := testPublisher(func(*http.Request) (*http.Response, error) {
		t.Error("invalid event sent")
		return nil, errors.New("unexpected request")
	})
	p.Emit(t.Context(), Event{})
}
