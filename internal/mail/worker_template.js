import PostalMime from "postal-mime";

// autokey-inbox worker: catch-all -> POST to autokey /v1/inbox, optional forward.
// Env: AUTOKEY_INBOX_URL, AUTOKEY_BEARER, FALLBACK_FORWARD (optional).
export default {
  async email(message, env, ctx) {
    let subject = message.headers.get("subject") || "";
    let text = "";
    let html = "";
    try {
      const raw = await new Response(message.raw).arrayBuffer();
      const parsed = await new PostalMime().parse(raw);
      subject = parsed.subject || subject;
      text = parsed.text || "";
      html = parsed.html || "";
    } catch (e) {
      // fall through with headers only
    }
    const body = JSON.stringify({
      envelope_to: message.to,
      from: message.from,
      subject,
      text,
      html,
      message_id: message.headers.get("message-id") || "",
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
