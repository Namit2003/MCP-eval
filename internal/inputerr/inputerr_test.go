package inputerr

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDescribeSyntaxPosition(t *testing.T) {
	data := []byte("{\n  \"a\": 1,\n  \"b\": ]\n}")
	var v any
	err := Describe(data, json.Unmarshal(data, &v))
	if err == nil || err.Error() != "invalid JSON at line 3, column 8: invalid character ']' looking for beginning of value" {
		t.Errorf("got %v", err)
	}
}

func TestDescribePassesOtherErrors(t *testing.T) {
	other := errors.New("boom")
	if got := Describe(nil, other); got != other {
		t.Errorf("got %v", got)
	}
}

func TestPrefix(t *testing.T) {
	if Prefix("f", nil) != nil {
		t.Error("nil should stay nil")
	}
	if got := Prefix("f", errors.New("x")).Error(); got != "f: x" {
		t.Errorf("single: got %q", got)
	}
	if got := Prefix("f", errors.Join(errors.New("x"), errors.New("y"))).Error(); got != "f: x\nf: y" {
		t.Errorf("joined: got %q", got)
	}
}
