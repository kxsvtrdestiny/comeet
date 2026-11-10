export interface User {
  id: string;
  username: string;
  displayName: string;
}
export interface Session {
  token: string;
  expiresAt: number;
  user: User;
}
export interface Meeting {
  id: string;
  meetingNo: string;
  title: string;
  hostUserId: string;
  hostName: string;
  status: "active" | "ending" | "ended";
  startedAt: number;
  endedAt: number | null;
}
export interface Preparation {
  id: string;
  meetingNo: string;
  title: string;
  hostName: string;
  status: string;
}
export interface MeetingList {
  items: Meeting[];
  nextCursor: string;
  hasMore: boolean;
}
export const staticPreview = import.meta.env.MODE === "pages";
export const formatMeetingNo = (no: string) =>
  no.replace(/(\d{3})(\d{3})(\d{3})/, "$1-$2-$3");
