package serviceruntime_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/josexy/flowlens/internal/serviceruntime"
	"github.com/josexy/flowlens/internal/serviceruntime/topictest"
)

func TestTopicDeliversInSubscriptionOrder(t *testing.T) {
	var topic serviceruntime.Topic[string]
	var other serviceruntime.Topic[string]
	var got []string
	topic.Subscribe(func(value string) { got = append(got, "first "+value) })
	topic.Subscribe(func(value string) { got = append(got, "second "+value) })
	other.Subscribe(func(string) { got = append(got, "other") })
	topic.Publish("a")
	if want := []string{"first a", "second a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery = %v, want %v", got, want)
	}
}

func TestTopicKeepsOnePublishersOrder(t *testing.T) {
	var topic serviceruntime.Topic[int]
	var got []int
	topic.Subscribe(func(value int) { got = append(got, value) })
	for i := range 100 {
		topic.Publish(i)
	}
	if len(got) != 100 {
		t.Fatalf("delivered %d values", len(got))
	}
	for i, value := range got {
		if value != i {
			t.Fatalf("got[%d] = %d", i, value)
		}
	}
}

func TestTopicUnsubscribeCanRepeat(t *testing.T) {
	var topic serviceruntime.Topic[int]
	calls := 0
	off := topic.Subscribe(func(int) { calls++ })
	topic.Publish(1)
	off()
	off()
	topic.Publish(2)
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestTopicUnsubscribeDuringDelivery(t *testing.T) {
	var topic serviceruntime.Topic[int]
	var off func()
	first, second := 0, 0
	off = topic.Subscribe(func(int) { first++; off() })
	topic.Subscribe(func(int) { second++ })
	topic.Publish(1)
	topic.Publish(2)
	if first != 1 || second != 2 {
		t.Fatalf("first = %d, second = %d", first, second)
	}
}

func TestTopicConcurrentPublishAndSubscribe(t *testing.T) {
	var topic serviceruntime.Topic[int]
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 200 {
				off := topic.Subscribe(func(int) {})
				topic.Publish(1)
				off()
			}
		})
	}
	wg.Wait()
}

func TestRecorderKeepsPublishedValues(t *testing.T) {
	var topic serviceruntime.Topic[int]
	recorder := topictest.Record(t, &topic)
	if _, ok := recorder.Last(); ok {
		t.Fatal("empty recorder has a last value")
	}
	topic.Publish(1)
	topic.Publish(3)
	if got := recorder.Values(); !reflect.DeepEqual(got, []int{1, 3}) || recorder.Len() != 2 {
		t.Fatalf("values = %v", got)
	}
	if last, ok := recorder.Last(); !ok || last != 3 {
		t.Fatalf("last = %d, %t", last, ok)
	}
}
