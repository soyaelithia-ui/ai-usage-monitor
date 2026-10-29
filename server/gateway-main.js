import { config } from './config.js';
import { gateway } from './gateway.js';
const c = config();
const server = gateway(c).listen(c.gatewayPort, '127.0.0.1', () =>
  console.log('AI Usage Monitor gateway ready'),
);
process.on('SIGTERM', () => server.close(() => process.exit(0)));
