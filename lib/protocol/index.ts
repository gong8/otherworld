export type { Envelope, Charter } from "./types";

// Frame is the gateway's wire shape: the seq is the reconnect cursor.
export type Frame = { seq: number; env: import("./types").Envelope };

export const principalOf = (agentVoice: string) =>
  "voice:principal:" + agentVoice.replace(/^voice:/, "").replace(/-agent$/, "");
