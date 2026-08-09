let csrfToken = "";
export const setCSRF = (value: string) => {
  csrfToken = value;
};

type APIErrorShape = { error?: { code?: string; message?: string } };
export class APIError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData))
    headers.set("Content-Type", "application/json");
  if (csrfToken && !["GET", "HEAD"].includes(init.method ?? "GET"))
    headers.set("X-CSRF-Token", csrfToken);
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
  });
  if (!response.ok) {
    let data: APIErrorShape = {};
    try {
      data = await response.json();
    } catch {
      /* empty */
    }
    throw new APIError(
      response.status,
      data.error?.code ?? "request_failed",
      data.error?.message ?? `요청 실패 (${response.status})`,
    );
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export function postJSON<T>(path: string, body: unknown) {
  return api<T>(path, { method: "POST", body: JSON.stringify(body) });
}
export function putJSON<T>(path: string, body: unknown) {
  return api<T>(path, { method: "PUT", body: JSON.stringify(body) });
}
export function patchJSON<T>(path: string, body: unknown) {
  return api<T>(path, { method: "PATCH", body: JSON.stringify(body) });
}
