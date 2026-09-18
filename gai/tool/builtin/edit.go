// Copyright 2024 CloudWeGo Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package builtin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/sofiworker/gk/gai/core"
)

// 替换逻辑改编自 Eino InMemoryBackend.Edit；存储与并发检查改为本 SDK 能力接口。
// Replacement logic is adapted from Eino InMemoryBackend.Edit; storage and concurrency checks use this SDK's capability interfaces.
func edit(ctx context.Context, tc core.ToolContext, a EditFileArgs) (core.ToolOutput, error) {
	writer, ok := tc.Files.(interface {
		CompareAndWrite(context.Context, core.Call, string, string, []byte) (core.Result, error)
	})
	if !ok {
		return core.ToolOutput{}, core.ErrUnsupported
	}
	data, err := tc.Files.ReadFile(ctx, tc.Call, a.Path)
	if err != nil {
		return core.ToolOutput{}, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if a.ExpectedContentID != "" && a.ExpectedContentID != hash {
		return core.ToolOutput{}, core.ErrConflict
	}
	content := string(data)
	if a.OldText == "" {
		return core.ToolOutput{}, fmt.Errorf("old_text must be non-empty")
	}
	if !strings.Contains(content, a.OldText) {
		return core.ToolOutput{}, fmt.Errorf("old_text not found")
	}
	if !a.ReplaceAll {
		firstIndex := strings.Index(content, a.OldText)
		if firstIndex != -1 && strings.Contains(content[firstIndex+len(a.OldText):], a.OldText) {
			return core.ToolOutput{}, fmt.Errorf("multiple occurrences require replace_all")
		}
	}
	var newContent string
	if a.ReplaceAll {
		newContent = strings.ReplaceAll(content, a.OldText, a.NewText)
	} else {
		newContent = strings.Replace(content, a.OldText, a.NewText, 1)
	}
	result, err := writer.CompareAndWrite(ctx, tc.Call, a.Path, hash, []byte(newContent))
	return mutationOutput(result, err)
}
