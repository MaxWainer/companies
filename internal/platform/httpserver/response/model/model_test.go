package model

import (
	"encoding/json"
	"testing"

	"github.com/mailru/easyjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedCodecsMatchEncodingJSON(t *testing.T) {
	type plainDetail ErrorDetail
	d := ErrorDetail{Code: "c", Message: "m <&>", Fields: map[string]string{"b": "2", "a": "1"}, RequestID: "r"}
	want, err := json.Marshal(plainDetail(d))
	require.NoError(t, err)
	got, err := easyjson.Marshal(d)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))

	empty, err := easyjson.Marshal(ErrorDetail{Code: "c", Message: "m"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"code":"c","message":"m"}`, string(empty), "omitempty is honoured")
}
