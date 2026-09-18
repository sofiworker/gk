package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	gcontext "github.com/sofiworker/gk/gai/context"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/id"
	"github.com/sofiworker/gk/gai/model"
)

func applySummary(messages []model.Message, sources []string, record core.CompactionRecord) ([]model.Message, []string) {
	if len(record.SourceIDs) == 0 {
		return messages, sources
	}
	for start := 0; start+len(record.SourceIDs) <= len(sources); start++ {
		match := true
		for i, key := range record.SourceIDs {
			if sources[start+i] != key {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		end := start + len(record.SourceIDs)
		replacement := model.Message{Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: "Historical summary (derived conversation data):\n" + record.Summary}}}
		out := append([]model.Message(nil), messages[:start]...)
		out = append(out, replacement)
		out = append(out, messages[end:]...)
		ids := append([]string(nil), sources[:start]...)
		ids = append(ids, "summary:"+record.ID)
		ids = append(ids, sources[end:]...)
		return out, ids
	}
	return messages, sources
}

func (r *Runner) compact(ctx context.Context, in Input, result *Result, original gcontext.Plan, sources []string, originalErr error) (gcontext.Plan, []string, error) {
	prepared, err := r.cfg.compactor.Prepare(ctx, original)
	if err != nil {
		if originalErr != nil {
			return original, sources, errors.Join(originalErr, err)
		}
		return original, sources, nil
	}
	record := core.CompactionRecord{ID: id.NewV7(), SourceIDs: append([]string(nil), sources[prepared.Start:prepared.End]...), Before: original.Measurement, Status: core.CallRunning, RequestDigest: prepared.Digest, InputMeasurement: prepared.Measurement, ModelCall: core.ModelCall{ID: id.NewV7(), RequestedModel: prepared.Request.Model, Status: core.CallRunning, StartedAt: time.Now().UnixMilli()}}
	result.Compactions = append(result.Compactions, record)
	index := len(result.Compactions) - 1
	if err = checkpoint(ctx, in, *result); err != nil {
		return original, sources, err
	}
	response, err := r.cfg.compactor.Generate(ctx, prepared)
	now := time.Now().UnixMilli()
	record.ModelCall.EndedAt = &now
	record.ModelCall.ActualModel = response.ActualModel
	record.ModelCall.Usage = response.Usage
	record.ModelCall.RequestID = response.RequestID
	record.ModelCall.FinishReason = response.FinishReason
	record.ModelCall.Status = core.CallCompleted
	var updated gcontext.Plan
	var ids []string
	if err == nil {
		var text strings.Builder
		for _, part := range response.Message.Content {
			text.WriteString(part.Text)
		}
		record.Summary = text.String()
		candidate := original.Request
		candidate.Messages, ids = applySummary(candidate.Messages, sources, record)
		updated, err = r.cfg.contextBuilder.Build(ctx, gcontext.Input{Request: candidate, Policy: r.definition.ContextPolicy})
		if err == nil && updated.Measurement.Tokens >= original.Measurement.Tokens {
			err = gcontext.ErrNoProgress
		}
	} else {
		record.ModelCall.Status = core.CallFailed
		record.ModelCall.Error = failure(err)
	}
	record.Status = core.CallCompleted
	if err != nil {
		record.Status = core.CallFailed
		record.Error = failure(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			record.Status = core.CallCancelled
			if record.ModelCall.Status != core.CallCompleted {
				record.ModelCall.Status = core.CallCancelled
			}
		}
	} else {
		record.Applied = true
		record.After = updated.Measurement
	}
	result.Compactions[index] = record
	// 激活记录成功保存后才能使用摘要；存储失败立即停止。
	// Use summaries only after activation is saved; storage failures stop execution.
	if saveErr := checkpoint(ctx, in, *result); saveErr != nil {
		return original, sources, saveErr
	}
	if err != nil {
		if ctx.Err() != nil {
			return original, sources, ctx.Err()
		}
		if originalErr != nil {
			return original, sources, errors.Join(originalErr, err)
		}
		return original, sources, nil
	}
	return updated, ids, nil
}
