export type OfficeZone = 'breakroom' | 'work' | 'server' | 'bug';

export const OFFICE_CANVAS = {
  width: 1280,
  height: 720,
};

export const OFFICE_ZONE_POINT: Record<OfficeZone, { x: number; y: number }> = {
  breakroom: { x: 1070, y: 610 },
  work: { x: 300, y: 365 },
  server: { x: 1010, y: 235 },
  bug: { x: 1125, y: 245 },
};

export const OFFICE_ZONE_SLOTS: Record<OfficeZone, Array<{ x: number; y: number }>> = {
  breakroom: [
    { x: 1020, y: 620 },
    { x: 1090, y: 620 },
    { x: 1150, y: 620 },
    { x: 1040, y: 670 },
    { x: 1110, y: 670 },
    { x: 1180, y: 670 },
  ],
  work: [
    { x: 240, y: 350 },
    { x: 300, y: 345 },
    { x: 360, y: 350 },
    { x: 420, y: 360 },
    { x: 260, y: 420 },
    { x: 320, y: 425 },
    { x: 380, y: 425 },
    { x: 440, y: 430 },
  ],
  server: [
    { x: 950, y: 225 },
    { x: 1000, y: 220 },
    { x: 1055, y: 220 },
    { x: 930, y: 285 },
    { x: 990, y: 285 },
  ],
  bug: [
    { x: 1100, y: 230 },
    { x: 1160, y: 230 },
    { x: 1210, y: 235 },
    { x: 1085, y: 290 },
    { x: 1145, y: 295 },
  ],
};
