export type ModelRouteDTO = {
  id: string;
  publicId: string;
  provider: "grok_build" | "grok_web" | "grok_console";
  upstreamModel: string;
  capability: "responses" | "chat" | "image" | "image_edit" | "video" | "tts" | "stt" | "realtime";
  origin: "catalog" | "discovered" | "manual";
  enabled: boolean;
  accountIds: string[];
  bindingMode: boolean;
  supportedAccounts: number;
  syncedAccounts: number;
  totalAccounts: number;
  capabilityKnown: boolean;
  available: boolean;
  qualityGuardState: QualityGuardModelState;
  lastSyncedAt?: string;
};

export type QualityGuardModelState = "enabled" | "disabled" | "unknown";

export type ModelEndpointCapability = "completions" | "responses" | "messages" | "image" | "image_edit" | "video" | "tts" | "stt" | "realtime";

export type ModelRouteGroupDTO = {
  key: string;
  routes: ModelRouteDTO[];
  endpointCapabilities: ModelEndpointCapability[];
};
