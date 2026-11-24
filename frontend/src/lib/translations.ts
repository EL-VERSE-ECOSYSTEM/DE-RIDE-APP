export const languages = ['EN', 'YO', 'HA', 'IG', 'PCM'] as const;
export type Language = (typeof languages)[number];

const copy: Record<Language, Record<string, string>> = {
  EN: {
    liveParks: 'Live Parks', how: 'How It Works', portals: '6 Portals', pricing: 'Pricing & Ranks', amebo: 'Amebo AI', buyData: 'Buy Data', login: 'Log In', getStarted: 'Get Started Free',
    eyebrow: 'NIGERIA IS MOVING · LIVE PARK NETWORK', headline: 'Stop waiting hours at the motor park.', headlineAccent: 'See live seats, book ahead & move smarter.',
    description: 'Real-time bus visibility across Ogun, Lagos & all 36 states + FCT—with offline QR tickets, last-mile bikes, dispatch riders, Amebo AI, and affordable data.',
    findRide: 'Find your next ride', state: 'STATE', origin: 'FROM PARK / AREA', destination: 'TO CITY / PARK', search: 'Search with Photon', findBuses: 'Find Live Buses', parkDirectory: 'Park directory:',
    verified: 'Verified park partners', offline: 'Offline-ready tickets', updates: 'Live trip updates', readAloud: 'Read page aloud',
  },
  YO: {
    liveParks: 'Àwọn Pákì Tó Ń Ṣiṣẹ́', how: 'Bí Ó Ṣe Ń Ṣiṣẹ́', portals: 'Ètò 6', pricing: 'Owó & Ìpele', amebo: 'Amebo AI', buyData: 'Ra Data', login: 'Wọlé', getStarted: 'Bẹ̀rẹ̀ Lófẹ̀ẹ́',
    eyebrow: 'NÀÍJÍRÍÀ Ń RÌN · NẸ́TÍWỌ́KÌ PÁKÌ', headline: 'Má ṣe dúró fún wákàtí ní ibùdó ọkọ.', headlineAccent: 'Wo àga tó wà, ṣe ìwé àṣẹ síwájú, kí o rìn dáadáa.',
    description: 'Wo ọkọ̀ akérò ní Ogun, Èkó àti gbogbo ìpínlẹ̀ 36 pẹ̀lú FCT—ìwé QR tí ń ṣiṣẹ́ láìsí nẹ́tíwọ́kì, alùpùpù, àwọn aṣojú ìfiránṣẹ́ àti data tó rọrùn.',
    findRide: 'Wá ìrìn àjò rẹ', state: 'ÌPÍNLẸ̀', origin: 'LÁTI PÁKÌ / ÀGBÈGBÈ', destination: 'ÌLÚ / PÁKÌ ÀJỌ', search: 'Wá pẹ̀lú Photon', findBuses: 'Wá Ọkọ̀ Tó Wà', parkDirectory: 'Àkójọ pákì:', verified: 'Àwọn pákì tí a fọwọ́ sí', offline: 'Ìwé àṣẹ tó ń ṣiṣẹ́ láìsí nẹ́tíwọ́kì', updates: 'Ìròyìn ìrìn àjò lẹ́sẹ̀kẹsẹ̀', readAloud: 'Ka ojú-ewé sókè',
  },
  HA: {
    liveParks: 'Tashoshin Mota Masu Aiki', how: 'Yadda Yake Aiki', portals: 'Sashe 6', pricing: 'Farashi da Matsayi', amebo: 'Amebo AI', buyData: 'Sayi Data', login: 'Shiga', getStarted: 'Fara Kyauta',
    eyebrow: 'NAJERIYA NA CI GABA · HANYAR TASHOSHIN MOTA', headline: 'Ka daina jira awanni a tashar mota.', headlineAccent: 'Duba kujerun da ke akwai, yi booking tun wuri.',
    description: 'Duba motocin haya a Ogun, Legas da jihohi 36 tare da FCT—tikitin QR mai aiki ba tare da intanet ba, babura da isar da kaya.',
    findRide: 'Nemo tafiyarka', state: 'JIHA', origin: 'DAGA TASHAR MOTA / YANKI', destination: 'ZUWA BIRNI / TASHAR MOTA', search: 'Nema da Photon', findBuses: 'Nemo Motoci', parkDirectory: 'Jerin tashoshi:', verified: 'Tashoshin mota da aka tantance', offline: 'Tikitin da ke aiki ba tare da intanet ba', updates: 'Sabbin bayanan tafiya kai tsaye', readAloud: 'Karanta shafin da murya',
  },
  IG: {
    liveParks: 'Ebe Ụgbọala Na-arụ Ọrụ', how: 'Otu O Si Arụ Ọrụ', portals: 'Ọnụ Ụzọ 6', pricing: 'Ọnụ Ego na Ọkwa', amebo: 'Amebo AI', buyData: 'Zụta Data', login: 'Banye', getStarted: 'Malite n’efu',
    eyebrow: 'NAỊJIRỊA NAAGA N’IHU · NETWỌK EBE ỤGBỌALA', headline: 'Kwụsị ichere ọtụtụ awa n’ọdụ ụgbọala.', headlineAccent: 'Lee oche dị, deba tupu oge eruo, gaa nke ọma.',
    description: 'Hụ ụgbọala n’Ogun, Lagos na steeti 36 tinyere FCT—tiketi QR na-arụ ọrụ n’enweghị ịntanetị, ọgba tum tum na nnyefe ngwugwu.',
    findRide: 'Chọta njem gị', state: 'STEETI', origin: 'SITE N’ỌDỤ / MPAGARA', destination: 'RUE OBO DỊ / ỌDỤ', search: 'Jiri Photon chọọ', findBuses: 'Chọta Ụgbọala', parkDirectory: 'Ndepụta ọdụ:', verified: 'Ndị mmekọ ọdụ ụgbọala enyochara', offline: 'Tiketi na-arụ ọrụ n’enweghị ịntanetị', updates: 'Ozi njem ozugbo', readAloud: 'Gụọ ibe a n’olu',
  },
  PCM: {
    liveParks: 'Parks Wey Dey Work', how: 'How E Dey Work', portals: '6 Portals', pricing: 'Price & Ranks', amebo: 'Amebo AI', buyData: 'Buy Data', login: 'Log In', getStarted: 'Start Free',
    eyebrow: 'NIGERIA DEY MOVE · LIVE PARK NETWORK', headline: 'No wait for hours for motor park again.', headlineAccent: 'See seats wey dey, book ahead make you move sharp.',
    description: 'See buses for Ogun, Lagos and all 36 states plus FCT—with offline QR tickets, last-mile bikes, dispatch riders, Amebo AI and affordable data.',
    findRide: 'Find your next ride', state: 'STATE', origin: 'FROM PARK / AREA', destination: 'TO CITY / PARK', search: 'Search with Photon', findBuses: 'Find Buses Wey Dey', parkDirectory: 'Park list:', verified: 'Verified park partners', offline: 'Tickets wey work offline', updates: 'Live trip updates', readAloud: 'Read this page aloud',
  },
};

export function translate(language: Language, key: string): string {
  return copy[language][key] ?? copy.EN[key] ?? key;
}

export const htmlLanguage: Record<Language, string> = { EN: 'en', YO: 'yo', HA: 'ha', IG: 'ig', PCM: 'en-NG' };
