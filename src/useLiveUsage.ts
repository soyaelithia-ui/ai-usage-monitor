import { useEffect, useState } from 'react';
import type { LiveUpdate, Snapshot } from '../shared/types';

export function useLiveUsage() {
  const [data, setData] = useState<Snapshot | null>(null);
  const [connection, setConnection] = useState<'connecting' | 'online' | 'offline'>('connecting');
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  useEffect(() => {
    const stream = new EventSource('/api/stream');
    stream.addEventListener('snapshot', message => {
      try {
        setData(JSON.parse(message.data) as Snapshot);
        setConnection('online');
        setUpdatedAt(new Date().toISOString());
      } catch { setConnection('offline'); }
    });
    stream.addEventListener('usage', message => {
      try {
        const update = JSON.parse(message.data) as LiveUpdate;
        setData(previous => {
          if (!previous || update.event.id <= previous.last_event_id) return previous;
          const events = [update.event, ...previous.events].slice(0, 100);
          return {
            totals: update.totals, agents: update.agents, models: update.models,
            events, last_event_id: update.event.id,
            next_before_id: previous.events.length >= 100 ? events.at(-1)!.id : previous.next_before_id,
          };
        });
        setUpdatedAt(update.event.received_at);
        setConnection('online');
      } catch { setConnection('offline'); }
    });
    stream.onerror = () => setConnection('offline');
    return () => stream.close();
  }, []);
  return { data, connection, updatedAt };
}
