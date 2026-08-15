import "@testing-library/jest-dom/vitest";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { App } from "./App";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("portal shell", () => {
  it("announces loading state with valid status semantics", async () => {
    const pending = new Promise<never>(() => {});
    vi.stubGlobal("fetch", vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ apiVersion:"portal.steadystate.dev/v1alpha1", observedAt:new Date().toISOString(), data:{ build:{version:"v1.0.0",portalVersion:"v1.0.0",portalAssetsDigest:"sha256:test"}, context:"local",profile:"full",repository:"saadabdullaah/steadystate",csrfToken:"csrf",mode:"Local owner",links:{} } }) })
      .mockReturnValue(pending));
    vi.stubGlobal("EventSource", class { onopen=()=>{};onerror=()=>{};addEventListener(){}close(){} });
    render(<App/>);
    expect(await screen.findByRole("status", { name: "Loading portal data" })).toBeInTheDocument();
  });

  it("renders the local-owner navigation after session metadata loads", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const path=String(input); let data:unknown;
      if(path.endsWith("/meta"))data={ build:{version:"v1.0.0",portalVersion:"v1.0.0",portalAssetsDigest:"sha256:test"}, context:"local",profile:"full",repository:"saadabdullaah/steadystate",csrfToken:"csrf",mode:"Local owner",links:{} };
      else if(path.endsWith("/catalog"))data={tenants:[]};
      else data={context:"local",profile:"full",repository:"saadabdullaah/steadystate",counts:{teams:0,services:0,applications:0,databases:0},health:"Healthy",resources:[]};
      return { ok: true, json: async () => ({ apiVersion:"portal.steadystate.dev/v1alpha1", observedAt:new Date().toISOString(), data }) };
    }));
    vi.stubGlobal("EventSource", class { onopen=()=>{};onerror=()=>{};addEventListener(){}close(){} });
    render(<App/>);
    expect(await screen.findByRole("navigation", {name:"Primary"})).toBeInTheDocument();
    expect(screen.getByText("Local owner console", { selector: ".mode" })).toBeInTheDocument();
  });
});
