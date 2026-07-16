export function bodyByteLength(body) {
  if (body === null || body === undefined) {
    return 0;
  }
  if (typeof body === "string") {
    return body.length;
  }
  if (typeof body.byteLength === "number") {
    return body.byteLength;
  }
  if (typeof body.length === "number") {
    return body.length;
  }
  return 0;
}

export function hasZIPSignature(body) {
  const bytes = bodyBytes(body);
  if (bytes.length < 4 || bytes[0] !== 0x50 || bytes[1] !== 0x4b) {
    return false;
  }
  return (
    (bytes[2] === 0x03 && bytes[3] === 0x04)
    || (bytes[2] === 0x05 && bytes[3] === 0x06)
    || (bytes[2] === 0x07 && bytes[3] === 0x08)
  );
}

export function looksLikeMavenPOM(body) {
  const text = String(body || "").replace(/^\uFEFF/, "").trimStart();
  if (!text.startsWith("<")) {
    return false;
  }
  const withoutDeclaration = text.replace(/^<\?xml[\s\S]*?\?>\s*/, "");
  const withoutComments = withoutDeclaration.replace(/^(?:<!--[\s\S]*?-->\s*)*/, "");
  return /^<project(?:\s|>)/.test(withoutComments);
}

function bodyBytes(body) {
  if (body === null || body === undefined) {
    return new Uint8Array(0);
  }
  if (typeof body === "string") {
    const bytes = new Uint8Array(body.length);
    for (let index = 0; index < body.length; index += 1) {
      bytes[index] = body.charCodeAt(index) & 0xff;
    }
    return bytes;
  }
  if (body.buffer && typeof body.byteLength === "number") {
    return new Uint8Array(body.buffer, body.byteOffset || 0, body.byteLength);
  }
  if (typeof body.byteLength === "number") {
    return new Uint8Array(body);
  }
  return new Uint8Array(0);
}

