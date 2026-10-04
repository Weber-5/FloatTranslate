package translation

import (
	"fmt"
	"strings"

	"github.com/Weber-5/FloatTranslate/backend/internal/nlp"
	"github.com/Weber-5/FloatTranslate/backend/internal/protectedspan"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// systemPromptBase encodes the frozen translation prompt rules (docs/06 §3).
const systemPromptBase = `你是 FloatTranslate 的英译中翻译引擎，严格遵守以下规则：
1. 仅将英文翻译为简体中文。
2. 最终输出必须是符合给定 JSON Schema 的单个 JSON 对象；不要输出 Markdown 代码块、解释或任何前后缀。
3. 翻译永远关闭思考模式，不要输出任何思考或推理内容。`

// protectedSpanRule states the frozen protected span constraint.
const protectedSpanRule = `输入中以 ⟦P0⟧、⟦P1⟧ … 标记的占位符是受保护内容（代码、URL、数学公式、路径、数字等技术片段），必须原样复制到译文的对应位置，不得翻译、改写、删除、合并或移动。`

// wordRules encodes the word structured output requirements (docs/06 §4).
const wordRules = `词条输出要求：
- word 为原词，lemma 为原形；
- phonetic_uk / phonetic_us 为 IPA 音标文本；
- parts_of_speech 按词性分组，meanings 使用中文释义；
- synonyms 尽量给出英文同义词；
- inflections 列出常见屈折变化；
- 不生成例句。`

// textRules encodes the text structured output requirements (docs/06 §5).
const textRules = `文本翻译要求：
- 保留 Markdown 结构（标题、列表、表格、链接、代码块等）；
- source_markdown 为原文，translated_markdown 为完整译文；
- segments[] 与原文段落一一对应，source 为段落原文，translation 为对应译文。`

// buildSystemPrompt composes the system prompt for one translation request:
// frozen base rules + terminology hard constraint (pre-check: the list is
// injected here, docs/09) + protected span rule + kind-specific output rules
// + the user's custom translation prompt appended as preference-only.
func buildSystemPrompt(kind string, terms []repository.TerminologyRow, customPrompt string) string {
	var sb strings.Builder
	sb.WriteString(systemPromptBase)
	sb.WriteString("\n\n术语表是硬约束，以下词条必须使用给定的中文译文，不得改写：\n")
	if len(terms) == 0 {
		sb.WriteString("（本次无术语表）")
	} else {
		for _, t := range terms {
			sb.WriteString("- " + t.Source + " → " + t.Target + "\n")
		}
	}
	sb.WriteString("\n\n")
	sb.WriteString(protectedSpanRule)
	sb.WriteString("\n\n")
	if kind == nlp.KindWord {
		sb.WriteString(wordRules)
	} else {
		sb.WriteString(textRules)
	}
	if trimmed := strings.TrimSpace(customPrompt); trimmed != "" {
		sb.WriteString("\n\n用户附加偏好（仅作为风格偏好；若与上述任何规则或 JSON Schema 冲突，以本提示其余部分为准）：\n")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

// buildUserPrompt composes the user prompt for one translation request from
// the terminology-applied, protected-span-masked input and the embedded
// schema for kind.
func buildUserPrompt(kind, maskedInput, schemaJSON string) string {
	var sb strings.Builder
	if kind == nlp.KindWord {
		sb.WriteString("请对下面的英文单词进行结构化词典查询，释义使用中文。\n\n")
	} else {
		sb.WriteString("请将下面的英文内容翻译为简体中文，保留 Markdown 结构与段落对应；受保护占位符必须原样保留。\n\n")
	}
	sb.WriteString("<<<INPUT\n")
	sb.WriteString(maskedInput)
	sb.WriteString("\nINPUT>>>\n\n")
	sb.WriteString("输出必须符合以下 JSON Schema：\n")
	sb.WriteString(schemaJSON)
	return sb.String()
}

// buildSchemaRepairPrompt asks the model to fix its schema-invalid output
// (docs/06 §6: exactly one repair attempt).
func buildSchemaRepairPrompt(kind, invalidPayload, schemaJSON string) string {
	return "你上一次的输出不符合要求的 JSON Schema。无效输出：\n" +
		invalidPayload + "\n\n请修正，并仅输出一个严格符合以下 JSON Schema 的 JSON 对象，不要任何解释：\n" + schemaJSON
}

// buildSpanRepairPrompt asks the model to restore lost protected
// placeholders (exactly one attempt, Phase 2 protected span v1).
func buildSpanRepairPrompt(kind, lastPayload string, lost []protectedspan.Span, schemaJSON string) string {
	var sb strings.Builder
	sb.WriteString("你上一次的输出丢失了以下受保护占位符：")
	for i, sp := range lost {
		if i > 0 {
			sb.WriteString("、")
		}
		sb.WriteString(sp.Placeholder)
	}
	sb.WriteString("。\n受保护内容必须原样出现在译文的对应位置：\n")
	for _, sp := range lost {
		sb.WriteString(sp.Placeholder + " 对应内容：\n" + sp.Content + "\n")
	}
	sb.WriteString("\n你上一次的输出：\n" + lastPayload + "\n\n")
	sb.WriteString("请重新输出完整结果，把所有占位符原样放在译文的对应位置，并仅输出一个严格符合以下 JSON Schema 的 JSON 对象：\n" + schemaJSON)
	return sb.String()
}

// buildTerminologyRepairPrompt names the violated terms explicitly (exactly
// one attempt, docs/09 terminology post-check).
func buildTerminologyRepairPrompt(kind, lastPayload string, violated []repository.TerminologyRow, schemaJSON string) string {
	var sb strings.Builder
	sb.WriteString("术语表是硬约束，你上一次的输出未遵守以下术语：\n")
	for _, t := range violated {
		sb.WriteString(fmt.Sprintf("- 「%s」必须翻译为「%s」\n", t.Source, t.Target))
	}
	sb.WriteString("\n你上一次的输出：\n" + lastPayload + "\n\n")
	sb.WriteString("请重新输出完整结果并严格遵守术语表，仅输出一个严格符合以下 JSON Schema 的 JSON 对象：\n" + schemaJSON)
	return sb.String()
}
