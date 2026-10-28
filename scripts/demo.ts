import { randomUUID } from 'node:crypto';
import { setTimeout } from 'node:timers/promises';

const endpoint = process.env.API_URL ?? 'http://127.0.0.1:3000';
const models = ['gpt-6.1-sol', 'gpt-6-luna', 'claude-opus-5-5', 'claude-sonnet-5-5', 'gemini-3.8-flash', 'gemini-3.5-flash-lite'];
const agents = ['demo/investigador', 'demo/programador', 'demo/revisor', 'demo/coordinador'];
console.log(`Se enviarán 30 eventos de demostración a ${endpoint}. Se guardarán en su base de datos.`);
for (let index = 0; index < 30; index++) {
  const response = await fetch(`${endpoint}/api/usage`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      agent_name: agents[index % agents.length], model: models[index % models.length],
      prompt_tokens: 800 + index * 317, completion_tokens: 100 + index * 73,
      request_id: `demo-${randomUUID()}`,
    }),
  });
  if (!response.ok) throw new Error(`Error ${response.status}: ${await response.text()}`);
  const result = await response.json();
  console.log(`${index + 1}/30 · ${result.event.agent_name} · $${result.event.cost_usd ?? 'sin tarifa'}`);
  await setTimeout(250);
}
