// BlobCache holds the object URLs of the page's fetched pictures (a src cannot send a token, so
// they are fetched and shown from a blob). Two kinds:
//   - shared, by a key that names the content (a channel picture's id): kept in least-recently-used
//     order up to a budget of bytes, so showing the same picture again (the panel's grid after the
//     channel) does not fetch it again, and what is not used leaves;
//   - owned by one <img> (a picture that has no content name, as a file of an agent's, or the QR
//     code): let go of with its img, or when the img gets another.
// A URL let go of is revoked at once, unless an <img> of the page still shows it (`shown`): it is
// revoked by `sweep`, once nothing does. Every URL it creates goes by one of these two ways.
"use strict";

class BlobCache {
  constructor({ budget, create = (b) => URL.createObjectURL(b), revoke = (u) => URL.revokeObjectURL(u), shown = () => false }) {
    this.budget = budget;
    this.create = create;
    this.revoke = revoke;
    this.shown = shown;
    this.shared = new Map(); // key → { url, size }, least recently used first
    this.bytes = 0;
    this.pending = new Map(); // key → the fetch in flight
    this.retired = new Set(); // URLs let go of while shown: revoked by sweep
    this.owned = new Map(); // img → its URL
  }

  // fetch is the URL for key: the one cached, or what get() (a promise of a Blob) makes of it.
  fetch(key, get) {
    const hit = this.shared.get(key);
    if (hit) {
      this.shared.delete(key); // now the most recently used
      this.shared.set(key, hit);
      return Promise.resolve(hit.url);
    }
    let p = this.pending.get(key);
    if (!p) {
      p = get().then((blob) => this.put(key, blob)).finally(() => this.pending.delete(key));
      this.pending.set(key, p);
    }
    return p;
  }

  // put keeps blob under key, and lets go of the least recently used while over the budget (never
  // of the one just kept: a single picture bigger than the budget is shown).
  put(key, blob) {
    if (this.shared.has(key)) this.drop(key);
    const url = this.create(blob);
    this.shared.set(key, { url, size: blob.size });
    this.bytes += blob.size;
    while (this.bytes > this.budget && this.shared.size > 1) this.drop(this.shared.keys().next().value);
    return url;
  }

  drop(key) {
    const e = this.shared.get(key);
    if (!e) return;
    this.shared.delete(key);
    this.bytes -= e.size;
    this.release(e.url);
  }

  // own makes a URL for img alone; the one it had is let go of.
  own(img, blob) {
    const url = this.create(blob);
    const old = this.owned.get(img);
    if (old) this.release(old);
    this.owned.set(img, url);
    return url;
  }

  release(url) {
    if (this.shown(url)) this.retired.add(url);
    else this.revoke(url);
  }

  // sweep revokes what was let go of and is shown no more, and what belonged to an img that left the page.
  sweep() {
    for (const u of [...this.retired]) {
      if (this.shown(u)) continue;
      this.revoke(u);
      this.retired.delete(u);
    }
    for (const [img, u] of [...this.owned]) {
      if (img.isConnected || this.shown(u)) continue;
      this.revoke(u);
      this.owned.delete(img);
    }
  }
}

if (typeof module !== "undefined") module.exports = { BlobCache };
