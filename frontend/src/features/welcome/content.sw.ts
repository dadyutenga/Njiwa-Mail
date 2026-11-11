import { content } from "./content";

export const contentSw = {
  ...content,
  nav: {
    features: "Vipengele",
    how: "Jinsi inavyofanya kazi",
    docs: "Nyaraka",
    github: "GitHub",
    signIn: "Ingia",
    menu: "Fungua menyu",
    closeMenu: "Funga menyu",
  },
  hero: {
    eyebrow: "BARUA PEPE YA MUAMALA UNAYOIJISIMAMIA",
    title: "Mbebaji wako wa barua pepe.",
    description:
      "Njiwa-Mail ni jukwaa huria la barua pepe kwa programu zako. Tuma, pokea na fuatilia barua pepe kupitia API moja, kwenye seva unazodhibiti.",
    github: "Tazama kwenye GitHub",
    copy: "Nakili ombi",
    copied: "Imenakiliwa",
    response: "Jibu",
    stamp: "KUTOKA SEVA YAKO · KUELEKEA DUNIANI",
  },
  how: {
    eyebrow: "01 / NJIA",
    title: "Njia wazi kwa kila ujumbe.",
    description:
      "Programu yako inakabidhi ujumbe kwa Njiwa-Mail. Seva yako ya barua pepe inautuma mbele. Majibu na barua zilizorejeshwa hurudi.",
    nodes: ["Programu yako", "Njiwa-Mail", "Seva yako ya barua", "Mpokeaji"],
    return: "Majibu na barua zilizorejeshwa hurudi",
  },
  features: {
    eyebrow: "02 / INACHOFANYA",
    title: "Imejengwa kwa ujumbe muhimu.",
    items: [
      {
        title: "Tuma kwa HTTP au SMTP",
        text: "Ombi moja la API au mipangilio ya kawaida ya barua pepe ya mfumo wako. Vyote hupitia foleni na sheria zilezile.",
      },
      {
        title: "Haitumi mara mbili",
        text: "Kila ombi lina ufunguo wa kutotoka nakala, hivyo jaribio la pili kutoka programu yako haliwezi kutengeneza barua pepe nyingine.",
      },
      {
        title: "Majaribio salama",
        text: "Njiwa-Mail inajaribu tena pale tu inapokuwa na uhakika seva ya barua haikukubali ujumbe. Ikishindwa kujua, inasimama na kukuambia.",
      },
      {
        title: "Pokea barua pepe pia",
        text: "Majibu na barua zinazoingia hufika zikiwa JSON, pamoja na viambatisho, kupitia API au webhook.",
      },
      {
        title: "Barua zilizorejeshwa na malalamiko",
        text: "Anwani mbovu na ripoti za spam huchukuliwa na kuzuiwa kiotomatiki, ili sifa ya mtumaji ibaki safi.",
      },
      {
        title: "Webhooks zilizosainiwa",
        text: "Programu yako huelezwa kilichotokea kwa kila ujumbe, kwa maombi yaliyosainiwa, majaribio na urudiaji.",
      },
      {
        title: "Violezo kwa Kiingereza na Kiswahili",
        text: "Violezo vyenye matoleo na onyesho la moja kwa moja. Toleo lililochapishwa halibadiliki chini ya ujumbe ulio kwenye foleni.",
      },
      {
        title: "Foleni ya haki",
        text: "Kila programu hupata zamu yake, hivyo mlipuko wa programu moja hauwezi kuchelewesha misimbo ya kuingia ya nyingine.",
      },
    ],
  },
  status: {
    eyebrow: "03 / HALI YA UKWELI",
    title: "Jua hasa kilichotokea.",
    description: "Njiwa-Mail haidai kamwe “imefika”. Inaonyesha inachoweza kuthibitisha.",
    states: [
      { title: "Imewekwa foleni", text: "Ujumbe umehifadhiwa salama." },
      { title: "Inawasilishwa", text: "Seva yako ya barua pepe inawasiliana." },
      { title: "Imekubaliwa", text: "Seva yako ya barua pepe imekubali ujumbe." },
      { title: "Imejirejesha au malalamiko", text: "Ishara ya baadaye hurekodiwa ikifika." },
    ],
  },
  developer: {
    eyebrow: "04 / KWA WATOA PROGRAMU",
    title: "Ombi moja. Foleni ya kudumu.",
    description:
      "Tumia seva yako kutuma ujumbe wa muamala. Kila tukio liwe na ufunguo wake wa kutotoka nakala, na uutumie tena unapojaribu.",
    guide: "Soma mwongozo wa uunganishaji",
    tabs: content.developer.tabs,
  },
  selfHost: {
    eyebrow: "05 / MIUNDOMBINU YAKO",
    title: "Seva yako. Sheria zako.",
    description:
      "Njiwa-Mail inaendeshwa kando ya seva yako ya barua pepe kwa Docker Compose. Programu zako zinabaki na API moja huku seva yako ikishughulikia utumaji.",
    quick: "Anza haraka",
    github: "Chunguza chanzo",
  },
  faq: {
    eyebrow: "06 / MASWALI",
    title: "Inafaa kujua.",
    items: [
      {
        question: "Njiwa-Mail ni nini?",
        answer:
          "Jukwaa huria la barua pepe unalojisimamia. Programu zako hutuma na kupokea barua pepe kupitia API moja, kwa kutumia seva yako ya barua.",
      },
      {
        question: "Je, nalipa kwa kila barua pepe?",
        answer: "Hapana. Unaiendesha kwenye seva yako, kwa hiyo hakuna ada kwa kila ujumbe.",
      },
      {
        question: "Je, inachukua nafasi ya seva yangu ya barua?",
        answer:
          "Hapana. Inakaa kando ya seva ya barua kama Mailcow, ambayo bado inafanya uwasilishaji halisi na sahihi ya DKIM.",
      },
      {
        question: "Naweza kutuma majarida?",
        answer:
          "Hapana. Njiwa-Mail ni kwa barua pepe za muamala: misimbo ya kuingia, kuweka upya nenosiri, risiti na taarifa.",
      },
      {
        question: "Ninajuaje barua pepe imefika?",
        answer:
          "Njiwa-Mail inaonyesha inachoweza kuthibitisha: kwamba seva yako ya barua imekubali ujumbe, na kama barua iliyorejeshwa au malalamiko yamerudi. Haidai kuwa iliingia kikashani au kusomwa.",
      },
      {
        question: "Naweza kujaribu bila kutuma barua pepe halisi?",
        answer: "Ndiyo. Funguo za sandbox hunasa ujumbe hapa na haziwasiliani na seva halisi ya barua.",
      },
    ],
  },
  footer: {
    description: "Barua pepe za muamala kwenye seva unazodhibiti.",
    origin: "Imejengwa Tanzania",
  },
  theme: { light: "Mandhari angavu", dark: "Mandhari nyeusi" },
};
