package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnmaskedErrorMessagePreservesClientURLOnly(t *testing.T) {
	imageURL := "https://cdn.example.com/generated/image.png?token=secret"
	err := NewError(
		fmt.Errorf("replicate adaptor: failed to download image from %s: failed to download image: HTTP 404", imageURL),
		ErrorCodeBadResponse,
		ErrOptionWithUnmaskedErrorMessage(),
	)

	assert.Contains(t, err.ToOpenAIError().Message, imageURL)
	assert.Contains(t, err.MaskSensitiveError(), "***")
}
