import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, qs, request } from "./client";

function mockFetch(status: number, body: unknown, headers: Record<string, string> = {}) {
  const fn = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

describe("request", () => {
  it("returns data, ETag and total count", async () => {
    mockFetch(200, [{ id: 1 }], { ETag: '"7"', "X-Total-Count": "42" });
    const res = await request<{ id: number }[]>("/api/v1/customers");
    expect(res.data).toEqual([{ id: 1 }]);
    expect(res.etag).toBe('"7"');
    expect(res.total).toBe(42);
  });

  it("sends If-Match and a JSON body on writes", async () => {
    const fetchFn = mockFetch(200, { ok: true });
    await request("/api/v2/terminals/SAT-00001/config", { method: "PUT", body: { a: 1 }, ifMatch: '"3"' });
    const [, init] = fetchFn.mock.calls[0] as [string, RequestInit];
    expect(init.method).toBe("PUT");
    expect((init.headers as Record<string, string>)["If-Match"]).toBe('"3"');
    expect(init.body).toBe('{"a":1}');
  });

  it("raises ApiError with the problem detail on failure", async () => {
    mockFetch(412, { title: "Precondition Failed", detail: "Configuration changed since revision 3" });
    const err = await request("/x").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).isConflict).toBe(true);
    expect((err as ApiError).message).toBe("Configuration changed since revision 3");
  });
});

describe("qs", () => {
  it("drops empty values", () => {
    expect(qs({ a: "x", b: "", c: undefined, d: 0 })).toBe("?a=x&d=0");
    expect(qs({})).toBe("");
  });
});
