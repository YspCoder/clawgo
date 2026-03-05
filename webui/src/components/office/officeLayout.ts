export type OfficeZone = 'breakroom' | 'work' | 'server' | 'bug';

export const OFFICE_CANVAS = {
  width: 1280,
  height: 720,
};

export const OFFICE_ZONE_POINT: Record<OfficeZone, { x: number; y: number }> = {
  breakroom: { x: 1070, y: 610 },
  work: { x: 640, y: 470 },
  server: { x: 820, y: 220 },
  bug: { x: 230, y: 210 },
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
    { x: 520, y: 470 },
    { x: 600, y: 470 },
    { x: 680, y: 470 },
    { x: 760, y: 470 },
    { x: 560, y: 530 },
    { x: 640, y: 530 },
    { x: 720, y: 530 },
    { x: 800, y: 530 },
  ],
  server: [
    { x: 760, y: 240 },
    { x: 830, y: 240 },
    { x: 900, y: 240 },
    { x: 780, y: 290 },
    { x: 850, y: 290 },
  ],
  bug: [
    { x: 180, y: 230 },
    { x: 240, y: 230 },
    { x: 300, y: 230 },
    { x: 210, y: 280 },
    { x: 270, y: 280 },
  ],
};

