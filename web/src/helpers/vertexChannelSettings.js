// Preserve express-mode API keys while clearing project IDs outside API-key Vertex channels.
export function readVertexChannelSettings(type, settings = {}) {
  const keyType = type === 41 ? settings.vertex_key_type || 'json' : 'json';
  return {
    vertex_key_type: keyType,
    vertex_api_version:
      type === 41 ? settings.vertex_api_version || 'v1' : 'v1',
    vertex_project_id:
      type === 41 && keyType === 'api_key'
        ? (settings.vertex_project_id || '').trim()
        : '',
  };
}

export function writeVertexChannelSettings(settings, inputs) {
  const next = { ...settings };
  delete next.vertex_key_type;
  delete next.vertex_project_id;
  delete next.vertex_api_version;
  const vertex = readVertexChannelSettings(inputs.type, inputs);
  if (inputs.type === 41) {
    next.vertex_key_type = vertex.vertex_key_type;
    if (vertex.vertex_api_version !== 'v1') {
      next.vertex_api_version = vertex.vertex_api_version;
    }
    if (vertex.vertex_project_id) {
      next.vertex_project_id = vertex.vertex_project_id;
    }
  }
  return next;
}
