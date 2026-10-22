export const ADVANCED_CUSTOM_CHANNEL_TYPE = 59;
export const GEMINI_RESPONSES_CONVERTER =
  'gemini_generate_content_to_openai_responses';

export const defaultAdvancedCustomRoute = (version = 'v1beta') => ({
  incoming_path: `/${version}/models/{model}:generateContent`,
  upstream_path: '/v1/responses',
  converter: GEMINI_RESPONSES_CONVERTER,
  auth: { type: 'header', name: 'Authorization', value: 'Bearer {key}' },
});

export const defaultAdvancedCustomConfig = () => ({
  advanced_routes: ['v1beta', 'v1'].map(defaultAdvancedCustomRoute),
});

export function writeAdvancedCustomSettings(settings, inputs) {
  const result = { ...settings };
  if (Number(inputs.type) === ADVANCED_CUSTOM_CHANNEL_TYPE) {
    result.advanced_custom =
      inputs.advanced_custom || defaultAdvancedCustomConfig();
    result.tool_loss_policy = inputs.tool_loss_policy || 'allow';
  } else {
    delete result.advanced_custom;
  }
  return result;
}
