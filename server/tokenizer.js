import net from 'node:net';
import { saveMeasurement } from './provenance.js';
export class Tokenizer {
  constructor(db, path, onChange = () => {}) {
    this.db = db;
    this.path = path;
    this.onChange = onChange;
    this.queue = [];
    this.pending = new Set();
    this.running = false;
    this.stopped = false;
    this.status = { state: 'starting', queued: 0 };
  }
  request(input) {
    return new Promise((resolve) => {
      let data = '',
        done = false;
      const socket = net.createConnection(this.path);
      const finish = (result) => {
        if (done) return;
        done = true;
        socket.destroy();
        resolve(result);
      };
      socket.setTimeout(10000, () => finish({ status: 'timeout' }));
      socket.on('error', () => finish({ status: 'unavailable' }));
      socket.on('connect', () => socket.write(JSON.stringify(input) + '\n'));
      socket.on('data', (b) => {
        data += b;
        if (data.length > 32768) return finish({ status: 'worker_error' });
        if (data.includes('\n')) {
          try {
            finish(JSON.parse(data));
          } catch {
            finish({ status: 'worker_error' });
          }
        }
      });
      socket.on('end', () => finish({ status: 'worker_error' }));
      socket.on('close', () => finish({ status: 'worker_error' }));
    });
  }
  async start() {
    const refresh = async () => {
      const result = await this.request({ operation: 'capabilities' });
      if (!this.stopped)
        this.status = {
          ...this.status,
          state: result.status,
          ...result,
          queued: this.queue.length,
        };
    };
    await refresh();
    this.timer = setInterval(refresh, 30000);
    this.timer.unref();
    return this;
  }
  enqueue(input, text) {
    if (this.stopped) return;
    if (!Number.isFinite(Date.parse(input.occurredAt))) return;
    const key = input.source + ':' + input.responseId;
    if (this.pending.has(key)) return;
    if (Buffer.byteLength(text) > 2 * 1024 * 1024) {
      saveMeasurement(this.db, input, { status: 'text_limit' });
      return;
    }
    if (this.queue.length >= 32) {
      saveMeasurement(this.db, input, { status: 'queue_limit' });
      return;
    }
    this.pending.add(key);
    this.queue.push({ key, input, text });
    this.status.queued = this.queue.length;
    this.drain();
  }
  async drain() {
    if (this.running || this.stopped) return;
    this.running = true;
    while (this.queue.length && !this.stopped) {
      const job = this.queue.shift();
      this.status.queued = this.queue.length;
      const result = await this.request({
        provider: job.input.provider,
        model: job.input.model,
        text: job.text,
      });
      job.text = '';
      if (!this.stopped) {
        saveMeasurement(this.db, job.input, result);
        this.status.lastResult = result.status;
        this.onChange();
      }
      this.pending.delete(job.key);
    }
    this.running = false;
  }
  stop() {
    this.stopped = true;
    clearInterval(this.timer);
    this.queue.length = 0;
  }
}
