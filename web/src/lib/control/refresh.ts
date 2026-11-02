import { getControl, type Request } from './client';

// Poll the complete private read model without overlapping requests or stale writes.
export function controlRefresh(request: Request, success: (value: Awaited<ReturnType<typeof getControl>>) => void, failure: (error: unknown) => void) {
  let controller: AbortController | undefined;
  let version = 0;
  let running = false;
  let disposed = false;
  function cancel() { version++; controller?.abort(); running = false; }
  return {
    cancel,
    dispose() { disposed = true; cancel(); },
    async run() {
      if (disposed || running) return;
      running = true;
      const current = ++version;
      const active = new AbortController();
      controller = active;
      try {
        const bound: Request = (input, init) => request(input, { ...init, signal: active.signal });
        const value = await getControl(bound);
        if (!disposed && current === version && !active.signal.aborted) success(value);
      } catch (error) {
        if (!disposed && current === version && !active.signal.aborted) failure(error);
      } finally {
        if (current === version) running = false;
      }
    }
  };
}
