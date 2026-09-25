// autokey-inbox worker: catch-all -> POST to autokey /v1/inbox, optional forward.
// Env: AUTOKEY_INBOX_URL, AUTOKEY_BEARER, FALLBACK_FORWARD (optional).
// Dependency-free: no npm imports, uploads via raw REST multipart.
export default {
  async email(message, env, ctx) {
    const raw = await new Response(message.raw).arrayBuffer();
    const text = new TextDecoder("utf-8").decode(raw);
    const parsed = parseEmail(text);
    const body = JSON.stringify({
      envelope_to: message.to,
      from: message.from,
      subject: parsed.subject || message.headers.get("subject") || "",
      text: parsed.text || "",
      html: parsed.html || "",
      message_id: parsed.messageId || message.headers.get("message-id") || "",
    });
    ctx.waitUntil(
      fetch(env.AUTOKEY_INBOX_URL, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          authorization: "Bearer " + env.AUTOKEY_BEARER,
        },
        body,
      })
    );
    if (env.FALLBACK_FORWARD) {
      ctx.waitUntil(message.forward(env.FALLBACK_FORWARD));
    }
  },
};

function parseEmail(raw) {
  // Split headers/body on first blank line.
  const idx = raw.search(/\r?\n\r?\n/);
  const headRaw = idx >= 0 ? raw.slice(0, idx) : raw;
  const bodyRaw = idx >= 0 ? raw.slice(idx).replace(/^\r?\n\r?\n/, "") : "";
  // Unfold continuation lines.
  const headers = {};
  let last = "";
  for (const line of headRaw.split(/\r?\n/)) {
    if (/^[ \t]/.test(line) && last) {
      headers[last] += " " + line.trim();
    } else {
      const m = line.match(/^([^:]+):\s*(.*)$/);
      if (m) {
        last = m[1].toLowerCase();
        headers[last] = m[2];
      }
    }
  }
  const out = {
    subject: headers["subject"] || "",
    messageId: headers["message-id"] || "",
    text: "",
    html: "",
  };
  const ct = headers["content-type"] || "";
  const bmatch = ct.match(/boundary="?([^";]+)"?/i);
  if (!bmatch) {
    // Single-part: treat as text (decode base64/quoted-printable best-effort).
    out.text = decodeBody(bodyRaw, headers["content-transfer-encoding"] || "");
    return out;
  }
  const boundary = "--" + bmatch[1];
  for (const part of bodyRaw.split(boundary)) {
    const pi = part.search(/\r?\n\r?\n/);
    if (pi < 0) continue;
    const ph = part.slice(0, pi).toLowerCase();
    let pb = part.slice(pi).replace(/^\r?\n\r?\n/, "").replace(/\r?\n--\s*$/, "");
    const enc = (ph.match(/content-transfer-encoding:\s*([^\s;]+)/) || [])[1] || "";
    pb = decodeBody(pb, enc);
    if (/content-type:\s*text\/html/.test(ph)) {
      if (!out.html) out.html = pb;
    } else if (/content-type:\s*text\/plain/.test(ph)) {
      if (!out.text) out.text = pb;
    }
  }
  return out;
}

function decodeBody(s, enc) {
  enc = (enc || "").toLowerCase().trim();
  try {
    if (enc === "base64") {
      const bin = atob(s.replace(/\s+/g, ""));
      const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
      return new TextDecoder("utf-8").decode(bytes);
    }
    if (enc === "quoted-printable") {
      return s
        .replace(/=\r?\n/g, "")
        .replace(/=([0-9A-Fa-f]{2})/g, (_, h) =>
          String.fromCharCode(parseInt(h, 16))
        );
    }
  } catch (_) {
    // fall through with raw
  }
  return s;
}
