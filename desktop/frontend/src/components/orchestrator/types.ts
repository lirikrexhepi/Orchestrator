export type ThinkingEffort = 'Low' | 'Medium' | 'High' | 'Ultra' | string;
export type ThinkingMode = 'normal' | 'thinking';

export interface OptionChoice {
  id: string;
  label: string;
  default?: boolean;
}

/** Mirrors domain.OptionDescriptor: what a CLI actually supports per model. */
export interface OptionDescriptor {
  id: string;
  label: string;
  type: 'select' | 'boolean';
  choices?: OptionChoice[];
  default?: unknown;
}

export interface AIModel {
  id: string;
  name: string;
  providerId: string;
  icon?: string;
  supportsThinking?: boolean;
  effortLevels?: ThinkingEffort[];
  defaultEffort?: ThinkingEffort;
  defaultMode?: ThinkingMode;
  description?: string;
  /** Raw backend descriptors, used to translate UI labels back into CLI flags. */
  options?: OptionDescriptor[];
}

export interface CLIAccount {
  id: string;
  name: string;
  authMode?: string;
  models?: AIModel[];
}

export interface CLIProvider {
  id: string;
  name: string;
  accounts?: CLIAccount[];
  icon?: string;
  description?: string;
  availability?: string;
  version?: string;
  message?: string;
}

export interface ModelSettings {
  effort: ThinkingEffort;
  mode: ThinkingMode;
}

export interface OrchestratorState {
  providers: CLIProvider[];
  models: AIModel[];
  selectedProviderId: string;
  selectedModelId: string;
  modelSettings: Record<string, ModelSettings>;
  messageText: string;
  isModelPickerOpen: boolean;
  isEffortPickerOpen: boolean;
  activeHoveredModelId: string | null;
}
