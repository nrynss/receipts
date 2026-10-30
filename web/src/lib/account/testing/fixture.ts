// Used only when Vite is running in development mode. No provider calls occur.
export function fixtureFetch(initial: string): typeof fetch {
  let state = initial;
  let paired = false;
  return async (input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.pathname : new URL(input.url).pathname;
    const ok = (value: unknown, status = 200) => new Response(JSON.stringify(value), {status, headers:{'Content-Type':'application/json'}});
    if (path === '/auth/account') return ok({login:'streamer'});
    if (path === '/auth/devices') return ok(paired ? [{id:'sample-source',created_at:1}] : []);
    if (path === '/account-api/status') return ok({state,can_start: state === 'not_listening'});
    if (path === '/account-api/start') { state='listening'; return new Response(null,{status:204}); }
    if (path === '/account-api/stop') { state='not_listening'; return new Response(null,{status:204}); }
    if (path === '/api/control/pause') { state=state==='paused'?'listening':'paused'; return new Response(null,{status:204}); }
    if (path === '/auth/pair/confirm') { paired=true; return new Response(null,{status:204}); }
    if (path.startsWith('/auth/devices/')) { paired=false; return new Response(null,{status:204}); }
    if (init?.method === 'POST') return new Response(null,{status:204});
    return new Response(null,{status:404});
  };
}
