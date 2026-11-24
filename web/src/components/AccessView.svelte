<script lang="ts">
  import { exchangeToken } from '../lib/api';
  import type { SessionStatus } from '../lib/types';

  interface Props {
    initialNotice?: string;
    onAuthenticated: (csrf: string, expiresAt: string, session?: SessionStatus) => void;
  }

  let { initialNotice = '', onAuthenticated }: Props = $props();

  let token = $state('');
  let errorMsg = $state('');
  let submitting = $state(false);

  const TOKEN_RE = /^[A-Za-z0-9_-]{43}$/;

  $effect(() => {
    if (initialNotice) {
      errorMsg = initialNotice;
    }
  });

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    errorMsg = '';
    const candidate = token.trim();
    token = '';
    if (!TOKEN_RE.test(candidate)) {
      errorMsg = 'Formato inválido: el token debe tener 43 caracteres base64url.';
      return;
    }
    submitting = true;
    try {
      const sess = await exchangeToken(candidate);
      onAuthenticated(sess.csrf, sess.expiresAt, sess);
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'No se pudo autenticar la sesión';
    } finally {
      submitting = false;
    }
  }
</script>

<div class="access-shell">
  <main class="access-card">
    <div class="access-brand">
      <span class="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 20 20" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M4 14v-4M8 14V6M12 14V9M16 14V5" />
        </svg>
      </span>
      <span>AI Usage Monitor</span>
    </div>

    <h1>Iniciar sesión</h1>

    <form id="access-form" onsubmit={handleSubmit} novalidate>
      <label for="token">Token de acceso</label>
      <input
        id="token"
        name="token"
        type="password"
        autocomplete="current-password"
        required
        spellcheck="false"
        maxlength="64"
        aria-describedby="token-hint"
        bind:value={token}
        placeholder="Introduce tu clave de acceso"
      />
      <button type="submit" class="access-cta" disabled={submitting}>
        {submitting ? 'Verificando…' : 'Abrir mi panel'}
      </button>
      <p id="error" class="access-error" role="alert" aria-live="assertive">{errorMsg}</p>
      <small id="token-hint" class="muted-text">Admite tokens de un solo uso, con TTL o permanentes.</small>
    </form>
  </main>
</div>
