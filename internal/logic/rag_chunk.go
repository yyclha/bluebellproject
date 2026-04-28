package logic

import (
	"strings"
	"unicode/utf8"
)

const (
	defaultChunkSize    = 300 // defaultChunkSize 表示每个文本分块的目标字符数。
	defaultChunkOverlap = 60  // defaultChunkOverlap 表示相邻文本分块之间保留的重叠字符数。
)

// TextChunk 表示一段可独立向量化和召回的文本切片。
type TextChunk struct {
	Index int    // Index 是 chunk 在原文中的顺序编号，从 0 开始。
	Text  string // Text 是当前 chunk 的实际文本内容。
}

// SplitTextToChunks 将长文本按固定窗口切成多个 chunk，便于向量检索命中更细粒度的语义片段。
func SplitTextToChunks(text string, chunkSize int, overlap int) []TextChunk {
	normalized := normalizeChunkText(text)
	if normalized == "" {
		return nil
	}

	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 5
	}
	if overlap >= chunkSize {
		overlap = 0
	}

	runes := []rune(normalized)
	if len(runes) <= chunkSize {
		return []TextChunk{{
			Index: 0,
			Text:  normalized,
		}}
	}

	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	chunks := make([]TextChunk, 0, (len(runes)+step-1)/step)
	for start := 0; start < len(runes); start += step {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		end = adjustChunkEnd(runes, start, end)
		if end <= start {
			end = minInt(len(runes), start+chunkSize)
		}

		chunkText := strings.TrimSpace(string(runes[start:end]))
		if chunkText != "" {
			chunks = append(chunks, TextChunk{
				Index: len(chunks),
				Text:  chunkText,
			})
		}

		if end >= len(runes) {
			break
		}

		nextStart := end - overlap
		if nextStart <= start {
			nextStart = start + step
		}
		start = nextStart - step
	}

	return chunks
}

// BuildPostRAGText 统一构造帖子用于向量化的原始文本，避免标题和正文在不同调用点拼接方式不一致。
func BuildPostRAGText(title string, content string) string {
	return strings.TrimSpace(strings.TrimSpace(title) + "\n" + strings.TrimSpace(content))
}

// normalizeChunkText 对文本做轻量清洗，减少多余空白对 chunk 边界的干扰。
func normalizeChunkText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// adjustChunkEnd 优先在换行、中文标点或空白位置截断，尽量避免把一句话硬切开。
func adjustChunkEnd(runes []rune, start int, end int) int {
	if end >= len(runes) {
		return len(runes)
	}

	windowStart := end - 40
	if windowStart < start {
		windowStart = start
	}

	best := -1
	for i := end; i > windowStart; i-- {
		if isChunkBoundary(runes[i-1]) {
			best = i
			break
		}
	}
	if best != -1 {
		return best
	}
	return end
}

// isChunkBoundary 判断某个字符是否适合作为 chunk 的自然分隔点。
func isChunkBoundary(r rune) bool {
	switch r {
	case '\n', '。', '！', '？', '；', ';', '.', '!', '?', ',', '，', '、', ':', '：':
		return true
	}
	return utf8.RuneLen(r) > 0 && (r == ' ' || r == '\t')
}

// minInt 返回两个整数中的较小值。
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
