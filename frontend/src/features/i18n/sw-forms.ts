// Kiswahili for website forms, the form inbox and spam filters. Keys are the exact
// English text passed to phrase(); lowercase keys cover values the server returns.
// Needs a native-speaker review, like the rest of the Kiswahili.
export const swForms: Record<string, string> = {
  "A verified sender address on your domain, used for replies.":
    "Anwani ya mtumaji iliyothibitishwa kwenye domain yako, inayotumika kwa majibu.",
  "Accept submissions": "Pokea ujumbe",
  "Add a rule": "Ongeza kanuni",
  "Add rule": "Ongeza kanuni",
  "Allowed websites": "Tovuti zinazoruhusiwa",
  Checked: "Imekaguliwa",
  "Create a form, list the websites allowed to post to it, then paste the snippet into your page.":
    "Tengeneza fomu, orodhesha tovuti zinazoruhusiwa kuituma, kisha bandika msimbo kwenye ukurasa wako.",
  "Delete this form?": "Futa fomu hii?",
  "Delete this rule?": "Futa kanuni hii?",
  "Delete this submission?": "Futa ujumbe huu?",
  Embed: "Weka kwenye tovuti",
  "Embed on your website": "Weka kwenye tovuti yako",
  Files: "Faili",
  Form: "Fomu",
  "Form deleted": "Fomu imefutwa",
  "Form inbox": "Sanduku la fomu",
  Forms: "Fomu",
  "Its submissions and files are deleted permanently and the form stops accepting messages.":
    "Ujumbe na faili zake vinafutwa kabisa na fomu inaacha kupokea ujumbe.",
  "Messages sent through your forms appear here as they arrive.":
    "Ujumbe uliotumwa kupitia fomu zako unaonekana hapa unapowasili.",
  "Messages sent through your website forms. Suspected spam is kept apart and never notifies you.":
    "Ujumbe uliotumwa kupitia fomu za tovuti yako. Spam inayoshukiwa huwekwa kando na haikutaarifu kamwe.",
  "New form": "Fomu mpya",
  "No forms": "Hakuna fomu",
  "No rules": "Hakuna kanuni",
  "No submissions": "Hakuna ujumbe",
  "Notification timing": "Wakati wa taarifa",
  Notify: "Taarifa",
  "Notify these addresses": "Taarifu anwani hizi",
  "One per line, for example https://www.example.com":
    "Moja kwa kila mstari, mfano https://www.example.com",
  "One per line. They are told when a message arrives.":
    "Moja kwa kila mstari. Wanaarifiwa ujumbe unapowasili.",
  "Only the websites you list can post to this form.":
    "Ni tovuti ulizoorodhesha tu zinazoweza kutuma kwenye fomu hii.",
  "Open items": "Zilizo wazi",
  "Optional. Where visitors go after sending.":
    "Hiari. Mahali wageni wanapopelekwa baada ya kutuma.",
  "Past submissions keep their result.": "Ujumbe wa awali unabaki na matokeo yake.",
  "Paste this into a page served from one of the allowed origins. Keep the hidden hp_website field: real visitors never fill it, bots do.":
    "Bandika hii kwenye ukurasa unaotolewa kutoka asili moja iliyoruhusiwa. Acha uwanja uliofichwa hp_website: wageni halisi hawaujazi, roboti huujaza.",
  Pattern: "Kigezo",
  "Receive messages from your website. Every submission is checked for spam, stored encrypted and shown in the form inbox.":
    "Pokea ujumbe kutoka tovuti yako. Kila ujumbe hukaguliwa kwa spam, huhifadhiwa kwa usimbaji fiche na kuonekana kwenye sanduku la fomu.",
  "Reply by email": "Jibu kwa barua pepe",
  "Require a one-time token (blocks scripts that skip the page)":
    "Hitaji tokeni ya mara moja (inazuia skripti zinazoruka ukurasa)",
  "Send reply": "Tuma jibu",
  "Spam check": "Ukaguzi wa spam",
  "Spam filters": "Vichujio vya spam",
  Submission: "Ujumbe",
  "Submission deleted": "Ujumbe umefutwa",
  Submissions: "Jumbe",
  "Thank-you page": "Ukurasa wa shukrani",
  "The built-in checks still run on every submission.":
    "Ukaguzi uliojengewa ndani bado unafanya kazi kwa kila ujumbe.",
  "The message and its files are deleted permanently.": "Ujumbe na faili zake vinafutwa kabisa.",
  "The reply is accepted for sending. Delivery is confirmed only by the message timeline.":
    "Jibu limekubaliwa kutumwa. Kufika kunathibitishwa na ratiba ya ujumbe pekee.",
  "Try it on past submissions": "Jaribu kwenye ujumbe wa awali",
  Unknown: "Haijulikani",
  Updated: "Imesasishwa",
  "Your own rules, applied to every form on top of the built-in checks. Try a rule against past submissions before you save it.":
    "Kanuni zako mwenyewe, zinazotumika kwa kila fomu juu ya ukaguzi uliojengewa ndani. Zijaribu kwenye ujumbe wa awali kabla ya kuhifadhi.",
  "has files": "ina faili",
  "would match": "ingelingana",
  // values shown as labels
  immediate: "mara moja",
  hourly: "kila saa",
  daily: "kila siku",
  new: "mpya",
  open: "wazi",
  replied: "imejibiwa",
  closed: "imefungwa",
  spam: "spam",
  accept: "imekubaliwa",
  flag: "weka alama",
  quarantine: "tenga",
  reject: "kataa",
  keyword: "neno kuu",
  phrase: "msemo",
  regex: "regex",
  ip: "IP",
  "Add a language": "Ongeza lugha",
  "Added to the HTML of every language. The version keeps its own copy, so later edits to the stylesheet do not change it.":
    "Huongezwa kwenye HTML ya kila lugha. Toleo linabaki na nakala yake, kwa hiyo mabadiliko ya baadaye ya stylesheet hayalibadilishi.",
  "Delete this stylesheet?": "Futa stylesheet hii?",
  "Empty uses the page title.": "Tupu hutumia kichwa cha ukurasa.",
  "Import a template": "Leta kiolezo",
  "Leave empty to keep the bundle's own slug.": "Acha tupu ili kubaki na slug ya kifurushi.",
  "New stylesheet": "Stylesheet mpya",
  "No stylesheets": "Hakuna stylesheet",
  "Plain CSS only: no @import, scripts or template variables.":
    "CSS ya kawaida tu: bila @import, script au vigeu vya kiolezo.",
  "Reusable messages with variables, in English, Kiswahili or any other language. Published versions never change; usable from Compose, Bulk send and the API by slug.":
    "Ujumbe unaoweza kutumika tena wenye vigeu, kwa Kiingereza, Kiswahili au lugha nyingine yoyote. Matoleo yaliyochapishwa hayabadiliki kamwe; yanaweza kutumika kutoka Andika, Tuma kwa wingi na API kwa slug.",
  "Saved versions keep the CSS they already have.":
    "Matoleo yaliyohifadhiwa yanabaki na CSS waliyonayo.",
  "Shared CSS you can add to any template. A saved version keeps its own copy: changing a stylesheet affects only the next draft that uses it.":
    "CSS ya pamoja unayoweza kuongeza kwenye kiolezo chochote. Toleo lililohifadhiwa linabaki na nakala yake: kubadilisha stylesheet kunaathiri rasimu inayofuata tu.",
  Stylesheet: "Stylesheet",
  Stylesheets: "Stylesheet",
  "That is not valid JSON.": "Hii si JSON halali.",
  "The import becomes a draft. Nothing is published until you have looked at it and published it.":
    "Kilicholetwa kinakuwa rasimu. Hakuna kinachochapishwa hadi uikague na kuichapisha.",
  "Without content in this language, requests for it fall back to English.":
    "Bila maudhui katika lugha hii, maombi yake yanatumia Kiingereza.",
  "Write variables as {{name}}. Scripts, forms and event handlers are removed; the plain-text version is made from the HTML.":
    "Andika vigeu kama {{name}}. Script, fomu na vishughulikia matukio huondolewa; toleo la maandishi matupu hutengenezwa kutoka kwenye HTML.",
  "HTML file": "Faili la HTML",
  "Template bundle (JSON)": "Kifurushi cha kiolezo (JSON)",
  HTML: "HTML",
  Bundle: "Kifurushi",
  Close: "Funga",
  None: "Hakuna",
};
