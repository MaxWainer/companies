//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"example.com/companies/internal/event/eventpb"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrentCreatesWithSameName(t *testing.T) {
	tok := token(t, "racer")
	name := uniqueName(t)

	const n = 10
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			candidate := name
			if i%2 == 1 {
				candidate = strings.ToUpper(name)
			}
			statuses[i] = call(t, http.MethodPost, "/v1/companies", tok, newCompany(candidate)).status
		})
	}
	wg.Wait()

	counts := map[int]int{}
	for _, s := range statuses {
		counts[s]++
	}
	assert.Equal(t, map[int]int{http.StatusCreated: 1, http.StatusConflict: n - 1}, counts)
}

func TestConcurrentPatchesAreSerialized(t *testing.T) {
	tok := token(t, "racer")
	created := call(t, http.MethodPost, "/v1/companies", tok, newCompany(uniqueName(t)))
	require.Equal(t, http.StatusCreated, created.status)
	id := created.id(t)
	path := "/v1/companies/" + id

	const n = 15
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			r := call(t, http.MethodPatch, path, tok, map[string]any{
				"amount_of_employees": 1000 + i,
				"description":         fmt.Sprintf("writer %d", i),
			})
			assert.Equal(t, http.StatusOK, r.status, r.body)
		})
	}
	wg.Wait()

	final := call(t, http.MethodGet, path, "", nil)
	require.Equal(t, http.StatusOK, final.status)

	events := readEvents(t, id, 1+n)
	assert.Equal(t, eventpb.EventType_EVENT_TYPE_COMPANY_CREATED, events[0].GetType())
	for _, ev := range events[1:] {
		assert.Equal(t, eventpb.EventType_EVENT_TYPE_COMPANY_UPDATED, ev.GetType())
	}
	last := events[len(events)-1].GetCompany()
	assert.EqualValues(t, final.body["amount_of_employees"], last.GetAmountOfEmployees())
	assert.Equal(t, final.body["description"], last.GetDescription())
}

func TestTraceContextReachesKafka(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	tok := token(t, "tracer")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, env.baseURL+"/v1/companies",
		strings.NewReader(`{"name":"`+uniqueName(t)+`","amount_of_employees":1,"registered":true,"type":"NON_PROFIT"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	id := strings.TrimPrefix(resp.Header.Get("Location"), "/v1/companies/")
	msg := readMessages(t, id, 1)[0]
	assert.Contains(t, header(msg, "traceparent"), traceID,
		"the event continues the trace of the request that caused it")
}

func readMessages(t *testing.T, companyID string, n int) []kafkago.Message {
	t.Helper()
	var out []kafkago.Message
	consume(t, func(msg kafkago.Message) bool {
		if string(msg.Key) == companyID {
			out = append(out, msg)
		}
		return len(out) < n
	})
	return out
}
