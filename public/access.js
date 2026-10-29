const form = document.querySelector('form'),
  input = document.querySelector('input'),
  button = document.querySelector('button'),
  error = document.querySelector('#error');
form.addEventListener('submit', async (e) => {
  e.preventDefault();
  button.disabled = true;
  error.textContent = '';
  try {
    const token = input.value.trim();
    input.value = '';
    const r = await fetch('/auth/exchange', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
      credentials: 'same-origin',
      cache: 'no-store',
    });
    if (!r.ok) {
      const data = await r.json();
      throw Error(data.error ?? 'No se pudo acceder');
    }
    location.replace('/');
  } catch (e) {
    error.textContent = e.message;
    button.disabled = false;
    input.focus();
  }
});

// A Strict cookie is withheld on an external navigation. Once this public page is
// loaded, a same-origin session query can restore access without consuming another token.
button.disabled = true;
fetch('/auth/session', { credentials: 'same-origin', cache: 'no-store' })
  .then((r) => {
    if (r.ok) {
      location.replace('/');
      return;
    }
    button.disabled = false;
  })
  .catch(() => {
    button.disabled = false;
  });
