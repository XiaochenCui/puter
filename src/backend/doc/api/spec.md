# API Specification

## AI Model Naming

### Scope

This rule applies to `model` field in AI-related API endpoints. Such as:

- `puter-chat-completion/complete`

### Rule

- 3 formats all AI models must follow:
  - `<model-name>` (e.g., `gpt-4o`)
  - `<vendor>/<model-name>` (e.g., `openai/gpt-4o`)
  - `<supplier>:<vendor>/<model-name>` (e.g., `azure:openai/gpt-4o`)
- Backend will pick a model when multiple candidates match the request. For instance, if a request specifies `gpt-4o`, the system will pick the most affordable model that matches `gpt-4o` from all available models.
- Backend will return an error on invalid model names and invalid formats.
- Available models are defined in this [list](https://puter.com/puterai/chat/models).
