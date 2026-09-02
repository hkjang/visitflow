export type Role =
  | "user"
  | "lobby"
  | "dept_manager"
  | "security"
  | "auditor"
  | "admin"
  | "super_admin";

export interface User {
  id: string;
  username: string;
  displayName: string;
  email?: string;
  employeeId?: string;
  departmentId?: string;
  siteScope?: string[];
  role: Role;
  source: "local" | "oidc";
  lastLoginAt?: string;
  delegateUserId?: string;
  delegateUntil?: string;
  approvalDelegate?: boolean;
}

export interface VersionInfo {
  version: string;
  commit: string;
  builtAt?: string;
}

export interface AuthConfig {
  serviceName: string;
  companyName: string;
  localEnabled: boolean;
  oidcEnabled: boolean;
  version: VersionInfo;
}

export interface Site {
  id: string;
  code: string;
  name: string;
  address: string;
  mapUrl?: string;
  timezone: string;
}
export interface Lobby {
  id: string;
  siteId: string;
  code: string;
  name: string;
  instructions?: string;
}
export interface Department {
  id: string;
  name: string;
  parentId?: string;
  color: string;
}
export interface VisitType {
  id: string;
  code: string;
  name: string;
  description?: string;
  requiresNda: boolean;
  requiresSafetyBriefing: boolean;
  requiresVehicle: boolean;
  requiresEquipment: boolean;
  requiresApproval: boolean;
  active: boolean;
  sortOrder: number;
}

export interface ReferenceData {
  sites: Site[];
  lobbies: Lobby[];
  departments: Department[];
  hosts?: { id: string; name: string; email?: string; departmentId?: string }[];
  visitTypes?: VisitType[];
  locales?: string[];
  defaultLocale?: string;
  selfRegistrationEnabled?: boolean;
}

export interface FrequentVisitor {
  id: string;
  name: string;
  phone: string;
  email?: string;
  company?: string;
  title?: string;
  vehicle?: string;
  equipment: string[];
  consent: boolean;
  consentedAt: string;
  createdAt: string;
  updatedAt: string;
  templateCount: number;
}

export interface VisitTemplatePayload {
  purpose?: string;
  placeDetail?: string;
  company?: string;
}

export interface VisitTemplate {
  id: string;
  name: string;
  payload: VisitTemplatePayload;
  frequentVisitorIds: string[];
  frequentVisitorCount: number;
  frequentVisitors?: FrequentVisitor[];
  createdAt: string;
  updatedAt: string;
}

export type VisitStatus =
  | "REQUESTED"
  | "PENDING_APPROVAL"
  | "APPROVED"
  | "SCHEDULED"
  | "ARRIVED"
  | "CHECKED_IN"
  | "CHECKED_OUT"
  | "CANCELLED"
  | "REJECTED"
  | "NO_SHOW";

export interface Visit {
  id: string;
  requestNo: string;
  hostUserId: string;
  hostName: string;
  departmentId?: string;
  departmentName?: string;
  siteId: string;
  siteName: string;
  lobbyId?: string;
  lobbyName?: string;
  startAt: string;
  endAt: string;
  purpose: string;
  placeDetail?: string;
  status: VisitStatus;
  source: string;
  visitTypeId?: string;
  visitTypeName?: string;
  visitorCount: number;
  primaryVisitor: string;
  company?: string;
  createdAt: string;
}

export interface LobbyVisitor {
  visitorVisitId: string;
  visitId: string;
  visitor: string;
  company?: string;
  host: string;
  department?: string;
  site: string;
  lobby?: string;
  startAt: string;
  endAt: string;
  status: VisitStatus;
  checkedInAt?: string;
}

export interface KioskDevice {
  id: string;
  name: string;
  siteId: string;
  siteName: string;
  lobbyId?: string;
  lobbyName?: string;
  prefix: string;
  expiresAt?: string;
  lastSeenAt?: string;
  revokedAt?: string;
  createdAt: string;
  active: boolean;
}

export interface OperationalMetrics {
  visitsToday: number;
  visitorsCurrent: number;
  pendingApproval: number;
  activeSessions: number;
  activeApiKeys: number;
  lockedAccounts: number;
  notifications: Record<string, number>;
  queueBacklog: number;
  queueOldestSeconds: number;
  schemaVersion: number;
  uptimeSeconds: number;
  counters: Record<string, number>;
}

export interface GuidePost {
  id: string;
  title: string;
  category: string;
  content?: string;
  excerpt?: string;
  published: boolean;
  pinned: boolean;
  authorName: string;
  publishedAt?: string;
  createdAt: string;
  updatedAt: string;
}
