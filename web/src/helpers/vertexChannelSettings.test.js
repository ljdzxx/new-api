import { expect, test } from 'bun:test';
import {
  readVertexChannelSettings,
  writeVertexChannelSettings,
} from './vertexChannelSettings';

test('Vertex API key project survives save and reload without changing other settings', () => {
  const saved = writeVertexChannelSettings(
    { allow_service_tier: false },
    {
      type: 41,
      vertex_key_type: 'api_key',
      vertex_project_id: ' project-123 ',
    },
  );
  expect(saved).toEqual({
    allow_service_tier: false,
    vertex_key_type: 'api_key',
    vertex_project_id: 'project-123',
  });
  expect(
    readVertexChannelSettings(41, JSON.parse(JSON.stringify(saved))),
  ).toEqual({
    vertex_key_type: 'api_key',
    vertex_project_id: 'project-123',
  });
});

test('express mode accepts an empty project and clears a saved project', () => {
  expect(
    writeVertexChannelSettings(
      { vertex_project_id: 'old' },
      {
        type: 41,
        vertex_key_type: 'api_key',
        vertex_project_id: ' ',
      },
    ),
  ).toEqual({ vertex_key_type: 'api_key' });
});

test('switching authentication mode or channel type removes stale Vertex fields', () => {
  const saved = { vertex_key_type: 'api_key', vertex_project_id: 'old' };
  expect(
    writeVertexChannelSettings(saved, {
      type: 41,
      vertex_key_type: 'json',
      vertex_project_id: 'old',
    }),
  ).toEqual({ vertex_key_type: 'json' });
  expect(writeVertexChannelSettings(saved, { type: 1, ...saved })).toEqual({});
  expect(readVertexChannelSettings(41, {})).toEqual({
    vertex_key_type: 'json',
    vertex_project_id: '',
  });
  expect(saved.vertex_project_id).toBe('old');
});
