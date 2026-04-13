package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

const maxMergeConfirmationBytes = 64

func readBoundedMergeConfirmationToken(r io.Reader, maxBytes int) (string, error) {
	if r == nil {
		return "", errors.New(errors.EInvalidArgument, "confirmation input is required")
	}
	if maxBytes <= 0 {
		maxBytes = maxMergeConfirmationBytes
	}

	data, err := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	if err != nil {
		return "", errors.Wrap(errors.EInternal, "failed to read merge confirmation input", err)
	}
	if len(data) > maxBytes {
		return "", errors.NewWithDetails(
			errors.EInvalidArgument,
			"confirmation input exceeds maximum length",
			map[string]string{
				"hint": "type 'merge' exactly",
			},
		)
	}

	token := string(data)
	if nl := strings.IndexAny(token, "\r\n"); nl >= 0 {
		token = token[:nl]
	}
	return strings.TrimSpace(token), nil
}

func resolveBoundedPromptInput(prompt, promptFile string, maxBytes int, missingPromptMessage, emptyPromptMessage string) (string, error) {
	if prompt != "" && promptFile != "" {
		return "", errors.New(errors.EUsage, "use either --prompt or --prompt-file, not both")
	}
	if prompt != "" {
		if len(prompt) > maxBytes {
			return "", errors.NewWithDetails(
				errors.EPromptTooLarge,
				fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", maxBytes, len(prompt)),
				map[string]string{
					"max_bytes": fmt.Sprintf("%d", maxBytes),
					"got_bytes": fmt.Sprintf("%d", len(prompt)),
				},
			)
		}
		return prompt, nil
	}
	if promptFile == "" {
		return "", errors.New(errors.EPromptRequired, missingPromptMessage)
	}

	f, err := os.Open(promptFile)
	if err != nil {
		return "", errors.WrapWithDetails(
			errors.EPromptRequired,
			"failed to read prompt file",
			err,
			map[string]string{"path": promptFile},
		)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return "", errors.WrapWithDetails(
			errors.EPromptRequired,
			"failed to read prompt file",
			err,
			map[string]string{"path": promptFile},
		)
	}
	if len(data) > maxBytes {
		return "", errors.NewWithDetails(
			errors.EPromptTooLarge,
			fmt.Sprintf("prompt exceeds maximum size of %d bytes (got %d)", maxBytes, len(data)),
			map[string]string{
				"path":      promptFile,
				"max_bytes": fmt.Sprintf("%d", maxBytes),
				"got_bytes": fmt.Sprintf("%d", len(data)),
			},
		)
	}
	if len(data) == 0 {
		return "", errors.New(errors.EPromptRequired, emptyPromptMessage)
	}
	return string(data), nil
}
