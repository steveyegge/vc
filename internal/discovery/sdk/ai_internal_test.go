package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssessCodeParsesStructuredJSON(t *testing.T) {
	var gotReq AIRequest
	stubCallAI(t, func(_ context.Context, req AIRequest) (*AIResponse, error) {
		gotReq = req
		return &AIResponse{
			Text:          `{"summary":"SQL injection risk","issues":["Unsanitized input reaches query"],"recommendations":["Use parameterized queries"],"confidence":0.91}`,
			TokensUsed:    321,
			EstimatedCost: 0.12,
		}, nil
	})

	assessment, err := AssessCode(context.Background(), "db.Query(query)", "security", AssessmentOptions{
		Focus:   "Look for SQL injection",
		Context: "HTTP handler",
	})
	require.NoError(t, err)

	assert.Contains(t, gotReq.Prompt, "Respond with ONLY raw JSON")
	assert.Contains(t, gotReq.Prompt, "Focus: Look for SQL injection")
	assert.Contains(t, gotReq.Prompt, "Context: HTTP handler")
	assert.Contains(t, gotReq.Prompt, "db.Query(query)")
	assert.Equal(t, assessmentSystemPrompt, gotReq.SystemPrompt)

	assert.Equal(t, "security", assessment.Category)
	assert.Equal(t, "SQL injection risk", assessment.Summary)
	assert.Equal(t, []string{"Unsanitized input reaches query"}, assessment.Issues)
	assert.Equal(t, []string{"Use parameterized queries"}, assessment.Recommendations)
	assert.Equal(t, 0.91, assessment.Confidence)
	assert.Equal(t, 321, assessment.TokensUsed)
	assert.Equal(t, 0.12, assessment.EstimatedCost)
}

func TestAssessCodeParsesFencedJSON(t *testing.T) {
	stubCallAI(t, func(_ context.Context, _ AIRequest) (*AIResponse, error) {
		return &AIResponse{
			Text: "```json\n{\"summary\":\"No issues found\",\"issues\":[],\"recommendations\":[],\"confidence\":0.74}\n```",
		}, nil
	})

	assessment, err := AssessCode(context.Background(), "fmt.Println(\"ok\")", "style", AssessmentOptions{})
	require.NoError(t, err)

	assert.Equal(t, "No issues found", assessment.Summary)
	require.NotNil(t, assessment.Issues)
	require.NotNil(t, assessment.Recommendations)
	assert.Empty(t, assessment.Issues)
	assert.Empty(t, assessment.Recommendations)
	assert.Equal(t, 0.74, assessment.Confidence)
}

func TestBatchAssessCodePreservesSnippetIDs(t *testing.T) {
	var gotReq AIRequest
	stubCallAI(t, func(_ context.Context, req AIRequest) (*AIResponse, error) {
		gotReq = req
		return &AIResponse{
			Text:          `{"assessments":[{"id":"handler1","summary":"No issues found","issues":[],"recommendations":[],"confidence":0.82},{"id":"handler2","summary":"Authentication bypass risk","issues":["Missing auth check"],"recommendations":["Require authentication middleware"],"confidence":0.94}]}`,
			TokensUsed:    200,
			EstimatedCost: 0.50,
		}, nil
	})

	snippets := []CodeSnippet{
		{ID: "handler1", Code: "func handler1() {}", Context: "GET /v1/users"},
		{ID: "handler2", Code: "func handler2() {}", Context: "POST /v1/admin"},
	}

	assessments, err := BatchAssessCode(context.Background(), snippets, "security", AssessmentOptions{
		Focus: "Look for missing auth checks",
	})
	require.NoError(t, err)

	assert.Contains(t, gotReq.Prompt, "id: handler1")
	assert.Contains(t, gotReq.Prompt, "id: handler2")
	assert.True(t, strings.Contains(gotReq.Prompt, "GET /v1/users"))
	assert.True(t, strings.Contains(gotReq.Prompt, "POST /v1/admin"))
	assert.Equal(t, assessmentSystemPrompt, gotReq.SystemPrompt)

	require.Len(t, assessments, 2)
	assert.Equal(t, "No issues found", assessments["handler1"].Summary)
	assert.Empty(t, assessments["handler1"].Issues)
	assert.Empty(t, assessments["handler1"].Recommendations)
	assert.Equal(t, 0.82, assessments["handler1"].Confidence)

	assert.Equal(t, "Authentication bypass risk", assessments["handler2"].Summary)
	assert.Equal(t, []string{"Missing auth check"}, assessments["handler2"].Issues)
	assert.Equal(t, []string{"Require authentication middleware"}, assessments["handler2"].Recommendations)
	assert.Equal(t, 0.94, assessments["handler2"].Confidence)
	assert.Equal(t, 100, assessments["handler2"].TokensUsed)
	assert.Equal(t, 0.25, assessments["handler2"].EstimatedCost)
}

func TestBatchAssessCodeErrorsOnMissingSnippet(t *testing.T) {
	stubCallAI(t, func(_ context.Context, _ AIRequest) (*AIResponse, error) {
		return &AIResponse{
			Text: `{"assessments":[{"id":"handler1","summary":"No issues found","issues":[],"recommendations":[],"confidence":0.80}]}`,
		}, nil
	})

	_, err := BatchAssessCode(context.Background(), []CodeSnippet{
		{ID: "handler1", Code: "func handler1() {}"},
		{ID: "handler2", Code: "func handler2() {}"},
	}, "security", AssessmentOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing assessment for snippet "handler2"`)
}

func stubCallAI(t *testing.T, fn func(context.Context, AIRequest) (*AIResponse, error)) {
	t.Helper()

	original := callAI
	callAI = fn
	t.Cleanup(func() {
		callAI = original
	})
}
