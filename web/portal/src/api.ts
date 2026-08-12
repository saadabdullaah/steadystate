export type Envelope<T> = { apiVersion: string; observedAt: string; data: T; warnings?: string[] };
export type PortalError = { error: { code: string; message: string; remediation: string; requestID: string } };

let csrf = "";

export function setCSRF(value: string) { csrf = value; }

export async function api<T>(path: string, init: RequestInit = {}): Promise<Envelope<T>> {
  const headers = new Headers(init.headers);
  if (init.body) {
    headers.set("Content-Type", "application/json");
    headers.set("X-SteadyState-CSRF", csrf);
  }
  const response = await fetch(`/api/v1/${path}`, { ...init, headers, credentials: "same-origin" });
  const payload = await response.json() as Envelope<T> | PortalError;
  if (!response.ok) {
    const failure = payload as PortalError;
    throw new Error(`${failure.error.message}|${failure.error.remediation}|${failure.error.requestID}`);
  }
  return payload as Envelope<T>;
}

export function errorParts(error: unknown) {
  const [message, remediation, requestID] = (error instanceof Error ? error.message : String(error)).split("|");
  return { message, remediation, requestID };
}
