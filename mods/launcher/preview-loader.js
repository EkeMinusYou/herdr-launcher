// Keep screen reads bounded while prioritizing the latest keyboard selection.
export class PreviewLoader {
  constructor({ readText, readColor, changed, capacity = 32 }) {
    this.readText = readText;
    this.readColor = readColor;
    this.changed = changed;
    this.capacity = capacity;
    this.cache = new Map();
    this.key = null;
    this.warmKeys = [];
    this.warmed = new Set();
    this.textBusy = null;
    this.textPending = null;
    this.colorBusy = null;
    this.colorPending = null;
  }

  store(key, snapshot) {
    const previous = this.cache.get(key);
    // A fast text read may finish after the colored screen. Keep its colors.
    const merged = { ...snapshot, rows: snapshot.rows ?? previous?.rows ?? null };
    this.cache.delete(key);
    this.cache.set(key, merged);
    while (this.cache.size > this.capacity) this.cache.delete(this.cache.keys().next().value);
    return merged;
  }

  publish(key, snapshot, cx) {
    if (key === this.key) this.changed(key, snapshot, cx);
  }

  request(key, warmKeys, cx) {
    const changed = key !== this.key;
    this.key = key;
    this.warmKeys = [...new Set(warmKeys.filter(Boolean))].slice(0, 2);
    if (changed) {
      const cached = this.cache.get(key);
      if (cached) {
        this.cache.delete(key);
        this.cache.set(key, cached);
      }
      this.changed(key, cached, cx);
    }
    if (!key) {
      this.colorPending = null;
      this.warm(cx);
      return;
    }
    const cached = this.cache.get(key);
    if (!cached?.rows) this.text(key, cx);
    if (this.colorBusy) {
      if (this.colorBusy !== key) this.colorPending = key;
    } else this.color(key, cx);
  }

  text(key, cx) {
    if (this.textBusy) {
      if (this.textBusy !== key) this.textPending = key;
      return;
    }
    this.textBusy = key;
    this.readText(key, cx, (snapshot, cx) => {
      this.textBusy = null;
      this.publish(key, this.store(key, snapshot), cx);
      const pending = this.textPending;
      this.textPending = null;
      if (pending && pending === this.key && pending !== key) this.text(pending, cx);
      else if (this.key && this.key !== key && !this.cache.has(this.key)) this.text(this.key, cx);
    });
  }

  color(key, cx) {
    this.colorBusy = key;
    this.warmed.add(key);
    this.readColor(key, cx, (snapshot, cx) => {
      this.colorBusy = null;
      if (snapshot) this.publish(key, this.store(key, snapshot), cx);
      else if (key === this.key) {
        const cached = this.cache.get(key);
        if (cached) this.cache.set(key, { ...cached, rows: null });
        this.text(key, cx);
      }
      const pending = this.colorPending;
      this.colorPending = null;
      if (pending && pending === this.key && pending !== key) this.color(pending, cx);
      else this.warm(cx);
    });
  }

  warm(cx) {
    if (this.colorBusy) return;
    const key = this.warmKeys.find(key => key !== this.key && !this.cache.get(key)?.rows && !this.warmed.has(key));
    if (key) this.color(key, cx);
  }
}
