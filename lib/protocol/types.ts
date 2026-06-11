// generated from proto/ — do not edit; npm run gen:protocol

export type OtherworldEnvelope = {
  [k: string]: unknown;
} & {
  v: 0;
  id: string;
  ts: string;
  from: string;
  serves: string;
  scope: string;
  to?: string[];
  kind: "say" | "hail" | "propose" | "accept" | "decline" | "withdraw" | "ask_principal" | "settle";
  exchange?: string;
  body?: string;
  terms?: {
    type: string;
    value: unknown;
  };
};

export interface OtherworldCharter {
  voice: string;
  serves: string;
  kind: "person" | "thing";
  interests: string;
  mandate: {
    may_propose_terms: string[];
    may_settle_without_principal: boolean;
    spend_limit_marks: number;
  };
}

// Clean aliases
export type Envelope = OtherworldEnvelope;
export type Charter = OtherworldCharter;
