const stamp = document.getElementById("stamp");

function label(value) {
  return String(value).replaceAll("_", " ");
}

function valueHTML(value) {
  if (value === null || value === undefined || value === "") return "-";
  if (Array.isArray(value)) return value.length ? value.map(valueHTML).join(", ") : "-";
  if (typeof value === "object") return `<pre>${JSON.stringify(value, null, 2)}</pre>`;
  return String(value);
}

function card(title, item, skip = []) {
  const fields = Object.entries(item).filter(([key]) => !skip.includes(key));
  return `<article class="item"><h2>${title}</h2><dl>${fields
    .map(([key, value]) => `<dt>${label(key)}</dt><dd>${valueHTML(value)}</dd>`)
    .join("")}</dl></article>`;
}

async function control(payload) {
  const response = await fetch("/api/control", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || response.status);
  return body;
}

function formValues(form) {
  return Object.fromEntries(new FormData(form).entries());
}

document.getElementById("publish-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const result = document.getElementById("publish-result");
  const values = formValues(event.target);
  try {
    const payload = {
      action: "publish-quality-rule",
      profile: values.profile,
      minimum_loudness: Number(values.minimum_loudness),
      maximum_loudness: Number(values.maximum_loudness),
      maximum_age_millis: Number(values.maximum_age_millis),
      minimum_sources: Number(values.minimum_sources),
      required_kinds: values.required_kinds.split(",").map((item) => item.trim()).filter(Boolean),
    };
    const body = await control(payload);
    result.textContent = `已发布 ${body.value.id}`;
    refresh();
  } catch (error) {
    result.textContent = `发布失败：${error.message}`;
  }
});

document.getElementById("exception-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const result = document.getElementById("exception-result");
  const values = formValues(event.target);
  try {
    const payload = {
      action: "grant-quality-exception",
      event_id: values.event_id.trim(),
      revision_id: values.revision_id.trim(),
      message: values.message,
      approved_by: values.approved_by,
      waive: values.waive.split(",").map((item) => item.trim()).filter(Boolean),
    };
    if (values.expires_in_millis) payload.expires_in_millis = Number(values.expires_in_millis);
    const body = await control(payload);
    result.textContent = `已批准例外 ${body.value.id}`;
    refresh();
  } catch (error) {
    result.textContent = `批准失败：${error.message}`;
  }
});

document.getElementById("replay-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const target = document.getElementById("replay-result");
  const values = formValues(event.target);
  try {
    const body = await control({ action: "reevaluate-quality", event_id: values.event_id.trim() });
    const entries = body.value.entries || [];
    target.innerHTML = entries.length
      ? entries
          .map((entry) =>
            card(
              `${entry.rule_id} · ${entry.unchanged ? "与当时结论一致" : "结论发生变化"}`,
              entry,
              ["recomputed"],
            ) + card(`${entry.rule_id} 重算明细`, entry.recomputed),
          )
          .join("")
      : `<p class="empty">没有历史放行记录</p>`;
  } catch (error) {
    target.innerHTML = `<p class="empty">重算失败：${error.message}</p>`;
  }
});

async function refresh() {
  try {
    const response = await fetch("/api/quality", { headers: { Accept: "application/json" } });
    const payload = await response.json();
    document.getElementById("rules").innerHTML = payload.rules.length
      ? payload.rules.map((rule) => card(`${rule.id}`, rule)).join("")
      : `<p class="empty">暂无规则</p>`;
    document.getElementById("bindings").innerHTML = payload.bindings.length
      ? payload.bindings.map((binding) => card(`${binding.event_id} @ ${binding.revision}`, binding)).join("")
      : `<p class="empty">暂无版本绑定</p>`;
    document.getElementById("exceptions").innerHTML = payload.exceptions.length
      ? payload.exceptions.map((exception) => card(exception.id, exception)).join("")
      : `<p class="empty">暂无例外批准</p>`;
    document.getElementById("decisions").innerHTML = payload.decisions.length
      ? payload.decisions
          .map((decision) =>
            card(
              `${decision.rule.id} · ${decision.permitted ? "放行" : "拒绝"}`,
              { revision: decision.revision, reason: decision.reason, decided_at: decision.decided_at },
            ),
          )
          .join("")
      : `<p class="empty">暂无放行决定</p>`;
    stamp.textContent = `状态 ${response.status} · ${new Date().toLocaleTimeString()}`;
  } catch (error) {
    stamp.textContent = `连接已中断：${error.message}`;
  }
}

refresh();
setInterval(refresh, 5000);
