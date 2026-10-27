// The addon's API, reached by relative URLs from the page at /addons/battery/ (or wherever the
// backend serves it). A writing request carries the kit's CSRF header; the session is the gate's
// cookie, which the browser sends on its own.

export class ApiError extends Error {
    constructor(
        readonly status: number,
        readonly code: string,
        message: string,
    ) {
        super(message);
    }
}

async function parse<T>(res: Response): Promise<T> {
    const text = await res.text();
    let body: unknown = text;
    try {
        body = text ? JSON.parse(text) : {};
    } catch {
        /* not JSON */
    }
    if (!res.ok) {
        const b = body as {error?: string; message?: string};
        throw new ApiError(res.status, b?.error ?? String(res.status), b?.message ?? res.statusText);
    }
    return body as T;
}

export function apiUrl(path: string): string {
    return new URL(path, document.baseURI).toString();
}

export async function get<T>(path: string): Promise<T> {
    const res = await fetch(apiUrl(path), {headers: {Accept: 'application/json'}, credentials: 'same-origin'});
    return parse<T>(res);
}

export async function post<T>(path: string, body: unknown): Promise<T> {
    return send<T>('POST', path, body);
}

export async function put<T>(path: string, body: unknown): Promise<T> {
    return send<T>('PUT', path, body);
}

export async function patch<T>(path: string, body: unknown): Promise<T> {
    return send<T>('PATCH', path, body);
}

export async function del(path: string): Promise<void> {
    await send<unknown>('DELETE', path, undefined);
}

async function send<T>(method: string, path: string, body: unknown): Promise<T> {
    const res = await fetch(apiUrl(path), {
        method,
        headers: {Accept: 'application/json', 'Content-Type': 'application/json', 'X-CCU-Addon-Request': '1'},
        credentials: 'same-origin',
        body: body === undefined ? undefined : JSON.stringify(body),
    });
    return parse<T>(res);
}
