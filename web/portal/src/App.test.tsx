import "@testing-library/jest-dom/vitest";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { App } from "./App";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("portal shell", () => {
  it("renders the local-owner navigation after session metadata loads", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({ apiVersion:"portal.steadystate.dev/v1alpha1", observedAt:new Date().toISOString(), data:{ build:{version:"v1.0.0",portalVersion:"v1.0.0",portalAssetsDigest:"sha256:test"}, context:"local",profile:"full",repository:"saadabdullaah/steadystate",csrfToken:"csrf",mode:"Local owner",links:{} } }) }));
    vi.stubGlobal("EventSource", class { onopen=()=>{};onerror=()=>{};addEventListener(){}close(){} });
    render(<App/>);
    expect(await screen.findByRole("navigation", {name:"Primary"})).toBeInTheDocument();
    expect(screen.getByText("Local owner")).toBeInTheDocument();
  });
});
