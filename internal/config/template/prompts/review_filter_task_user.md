### Task

Positively verify each candidate finding using its `origin`, current source, and diff. Its
`failure_mode`, `violated_contract`, `evidence`, severity, and exact code anchor must
form one concrete causal claim. Omit the candidate if any required dimension is
missing, contradicted, speculative, or cannot be independently established.

### Verification scope

{{verification_scope}}

### Current diff

```{{path}}
{{diff}}
```

### Current file

```{{path}}
{{current_file_content}}
```

### Repository-owned rule and requirement context

{{system_rule}}

{{requirement_background}}

### Candidate findings

{{comments}}

### Output

Return only the IDs of positively verified findings as a JSON array, with no prose:

```json
["c-0", "c-2"]
```

Return `[]` when no candidate is positively verified.
