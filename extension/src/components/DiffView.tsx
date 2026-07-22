// Renders the operations[] list grouped by OpType. See
// docs/architecture.md §10.2.

import type { Operation, OpType } from "../lib/types";
import OperationRow from "./OperationRow";

export interface DiffViewProps {
  operations: Operation[];
}

const GROUP_ORDER: OpType[] = ["DELETE", "CREATE", "MOVE", "UPDATE"];

const GROUP_TITLES: Record<OpType, string> = {
  DELETE: "Delete",
  CREATE: "Create",
  MOVE: "Move",
  UPDATE: "Update",
};

export default function DiffView({ operations }: DiffViewProps) {
  if (operations.length === 0) {
    return (
      <div style={{ padding: 16, color: "#64748b", fontSize: 13, textAlign: "center" }}>
        No changes — browser already matches the desired state.
      </div>
    );
  }

  const grouped = new Map<OpType, Operation[]>();
  for (const type of GROUP_ORDER) grouped.set(type, []);
  for (const op of operations) {
    grouped.get(op.type)?.push(op);
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {GROUP_ORDER.filter((type) => (grouped.get(type)?.length ?? 0) > 0).map((type) => (
        <div key={type} style={{ border: "1px solid #e2e8f0", borderRadius: 8, overflow: "hidden" }}>
          <div
            style={{
              padding: "6px 10px",
              background: "#f1f5f9",
              fontSize: 12,
              fontWeight: 700,
              color: "#334155",
              textTransform: "uppercase",
              letterSpacing: 0.4,
            }}
          >
            {GROUP_TITLES[type]} ({grouped.get(type)!.length})
          </div>
          <div>
            {grouped.get(type)!.map((op, idx) => (
              <OperationRow key={`${type}-${idx}-${op.targetPath.join("/")}`} operation={op} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
