// Pings GET /health, shows connected/disconnected + editable port
// (persisted in chrome.storage.local). See docs/architecture.md §10.2.

import { useEffect, useState } from "react";
import { getHealth, DEFAULT_PORT, STORAGE_PORT_KEY } from "../lib/api";

export interface ConnectionStatusProps {
  /** Called whenever the port changes or the health check completes. */
  onPortChange?: (port: number) => void;
  onStatusChange?: (connected: boolean) => void;
}

type Status = "checking" | "connected" | "disconnected";

export default function ConnectionStatus({
  onPortChange,
  onStatusChange,
}: ConnectionStatusProps) {
  const [port, setPort] = useState<number>(DEFAULT_PORT);
  const [status, setStatus] = useState<Status>("checking");
  const [markoVersion, setMarkoVersion] = useState<string | undefined>();

  // Load persisted port on mount.
  useEffect(() => {
    let cancelled = false;
    chrome.storage.local.get(STORAGE_PORT_KEY).then((stored) => {
      if (cancelled) return;
      const storedPort = stored[STORAGE_PORT_KEY];
      if (typeof storedPort === "number" && storedPort > 0) {
        setPort(storedPort);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  // Ping /health whenever the port changes.
  useEffect(() => {
    let cancelled = false;
    setStatus("checking");
    getHealth(port)
      .then((res) => {
        if (cancelled) return;
        setStatus("connected");
        setMarkoVersion(res.markoVersion);
        onStatusChange?.(true);
      })
      .catch(() => {
        if (cancelled) return;
        setStatus("disconnected");
        setMarkoVersion(undefined);
        onStatusChange?.(false);
      });
    onPortChange?.(port);
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [port]);

  function handlePortChange(e: React.ChangeEvent<HTMLInputElement>) {
    const next = Number(e.target.value);
    if (!Number.isFinite(next) || next <= 0) return;
    setPort(next);
    chrome.storage.local.set({ [STORAGE_PORT_KEY]: next });
  }

  const dotColor =
    status === "connected" ? "#22c55e" : status === "checking" ? "#f59e0b" : "#ef4444";
  const label =
    status === "connected"
      ? `Connected${markoVersion ? ` (marko ${markoVersion})` : ""}`
      : status === "checking"
        ? "Checking..."
        : "Disconnected";

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 10,
        padding: "8px 12px",
        borderRadius: 8,
        background: "#f8fafc",
        border: "1px solid #e2e8f0",
        fontSize: 13,
      }}
    >
      <span
        aria-hidden
        style={{
          width: 9,
          height: 9,
          borderRadius: "50%",
          background: dotColor,
          flexShrink: 0,
          boxShadow: `0 0 0 3px ${dotColor}22`,
        }}
      />
      <span style={{ color: "#334155", flexGrow: 1 }}>{label}</span>
      <label style={{ display: "flex", alignItems: "center", gap: 6, color: "#64748b" }}>
        Port
        <input
          type="number"
          value={port}
          onChange={handlePortChange}
          style={{
            width: 70,
            padding: "3px 6px",
            border: "1px solid #cbd5e1",
            borderRadius: 4,
            fontSize: 13,
          }}
        />
      </label>
    </div>
  );
}
