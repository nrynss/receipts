// One private status request at a time; stale or disposed replies cannot write UI state.
export function statusRefresh(request: typeof fetch, success: (value: Record<string, unknown>) => void, failure: () => void) {
  let controller: AbortController | undefined;
  let version = 0;
  let disposed = false;
  function cancel() { version++; controller?.abort(); }
  return {
    cancel,
    dispose() { disposed = true; cancel(); },
    async run() {
      cancel(); const current = version; const active = new AbortController(); controller = active;
      try {
        const response = await request('/account-api/status', {signal: active.signal});
        if (!response.ok) throw new Error('Status unavailable');
        const value: Record<string, unknown> = await response.json();
        if (!disposed && current === version) success(value);
      } catch {
        if (!disposed && current === version && !active.signal.aborted) failure();
      }
    }
  };
}
