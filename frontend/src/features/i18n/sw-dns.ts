// Kiswahili for the DNS setup wizard (Cloudflare). Keys are the exact English text passed to phrase().
// Needs a native-speaker review, like the rest of the Kiswahili.
export const swDns: Record<string, string> = {
  "API token": "Tokeni ya API",
  Action: "Kitendo",
  "Add domain": "Ongeza kikoa",
  After: "Baada",
  "Already in place, left unchanged": "Tayari zipo, hazijabadilishwa",
  "Already there": "Tayari ipo",
  "Analysis finished.": "Uchambuzi umekamilika.",
  Analyze: "Chambua",
  "Analyze again": "Chambua tena",
  "Analyze this zone": "Chambua eneo hili",
  "Analyzing…": "Inachambua…",
  Apply: "Tekeleza",
  "Apply changes": "Tekeleza mabadiliko",
  "Apply the approved DNS changes?": "Kutekeleza mabadiliko ya DNS yaliyoidhinishwa?",
  "Apply to Cloudflare": "Tekeleza kwenye Cloudflare",
  "Applying…": "Inatekeleza…",
  Approve: "Idhinisha",
  "Approve these exact changes": "Idhinisha mabadiliko haya hasa",
  "Approved by": "Imeidhinishwa na",
  "Approved.": "Imeidhinishwa.",
  "Cannot be automated": "Haiwezi kufanywa kiotomatiki",
  "Changes made to DNS": "Mabadiliko yaliyofanywa kwenye DNS",
  "Check now": "Kagua sasa",
  "Checking DNS…": "Inakagua DNS…",
  "Checking…": "Inakagua…",
  "Choose the Cloudflare zone that contains this domain. Njiwa-Mail reads it first and changes nothing yet.":
    "Chagua eneo la Cloudflare lenye kikoa hiki. Njiwa-Mail huliosoma kwanza na haibadilishi chochote bado.",
  Cloudflare: "Cloudflare",
  "Cloudflare connected.": "Cloudflare imeunganishwa.",
  "Cloudflare sign-in did not complete. Try again or use an API token.":
    "Kuingia kwenye Cloudflare hakukukamilika. Jaribu tena au tumia tokeni ya API.",
  "Confirm these changes": "Thibitisha mabadiliko haya",
  "Connect Cloudflare": "Unganisha Cloudflare",
  "Connect Cloudflare so Njiwa-Mail can add the mail records for you. You will review every change before anything is written. Nothing is changed in DNS until you approve it.":
    "Unganisha Cloudflare ili Njiwa-Mail iongeze rekodi za barua kwa niaba yako. Utapitia kila badiliko kabla ya chochote kuandikwa. Hakuna kinachobadilishwa kwenye DNS hadi uidhinishe.",
  "Connect with token": "Unganisha kwa tokeni",
  "Create a token in Cloudflare with Zone → DNS → Edit and Zone → Zone → Read, limited to the one zone you will use. Never paste a Global API Key or your password.":
    "Tengeneza tokeni kwenye Cloudflare yenye Zone → DNS → Edit na Zone → Zone → Read, iliyozuiwa kwenye eneo moja utakalotumia. Usibandike kamwe Global API Key au nenosiri lako.",
  "DNS can take a while to propagate. Njiwa-Mail checks again automatically, and keeps checking in the background for 48 hours.":
    "DNS inaweza kuchukua muda kusambaa. Njiwa-Mail hukagua tena kiotomatiki, na huendelea kukagua nyuma ya pazia kwa saa 48.",
  "DNS changed after you approved, so nothing was written. Review the updated plan and approve again.":
    "DNS ilibadilika baada ya kuidhinisha, kwa hiyo hakuna kilichoandikwa. Pitia mpango uliosasishwa kisha idhinisha tena.",
  "DNS setup progress": "Maendeleo ya usanidi wa DNS",
  "DNS zone": "Eneo la DNS",
  Disconnect: "Tenganisha",
  "Disconnect Cloudflare?": "Kutenganisha Cloudflare?",
  "Disconnected.": "Imetenganishwa.",
  "Existing email detected": "Barua pepe iliyopo imegunduliwa",
  Failed: "Imeshindwa",
  "Fix the blocking problem above, then analyze again.":
    "Rekebisha tatizo linalozuia lililo juu, kisha chambua tena.",
  "Hide manual setup": "Ficha usanidi wa mkono",
  "I understand that adding an MX record changes where mail for that name is delivered.":
    "Naelewa kuwa kuongeza rekodi ya MX kunabadilisha mahali barua za jina hilo zinapopelekwa.",
  "I understand that my existing SPF record will be edited in place to add the sending server, and that nothing else in it changes.":
    "Naelewa kuwa rekodi yangu ya SPF iliyopo itahaririwa hapohapo ili kuongeza seva ya kutuma, na kwamba hakuna kingine ndani yake kinachobadilika.",
  "If something goes wrong": "Likitokea tatizo",
  Loading: "Inapakia",
  "My DNS is somewhere else": "DNS yangu iko mahali pengine",
  "Name (optional)": "Jina (si lazima)",
  "Needs action": "Inahitaji hatua",
  "Needs you: conflicts": "Inakuhitaji: migongano",
  "New API token": "Tokeni mpya ya API",
  "Njiwa-Mail does not have a safe value for these. Publish them yourself.":
    "Njiwa-Mail haina thamani salama kwa hizi. Zichapishe wewe mwenyewe.",
  "Njiwa-Mail will not change these. Resolve them in Cloudflare, then analyze again.":
    "Njiwa-Mail haitabadilisha hizi. Zitatue kwenye Cloudflare, kisha chambua tena.",
  "No record was written.": "Hakuna rekodi iliyoandikwa.",
  Note: "Maelezo",
  "Nothing has been written. Review the exact changes below.":
    "Hakuna kilichoandikwa. Pitia mabadiliko hasa yaliyo hapa chini.",
  "Nothing is marked as done. Analyze again to see what remains, or roll back what was added.":
    "Hakuna kilichowekwa alama ya kukamilika. Chambua tena kuona kilichobaki, au rudisha nyuma kilichoongezwa.",
  Now: "Sasa",
  "Only records Njiwa-Mail created are removed, and an SPF record it edited is put back, but only while nobody has changed them since. Anything changed by someone else is left alone and listed. Mail that relies on these records may stop working.":
    "Ni rekodi zilizoundwa na Njiwa-Mail pekee ndizo huondolewa, na rekodi ya SPF iliyohaririwa hurudishwa, lakini tu ikiwa hakuna aliyezibadilisha tangu hapo. Chochote kilichobadilishwa na mtu mwingine huachwa na kuorodheshwa. Barua zinazotegemea rekodi hizi zinaweza kuacha kufanya kazi.",
  "Only some of the records were written. The setup is not complete.":
    "Ni baadhi tu ya rekodi zilizoandikwa. Usanidi haujakamilika.",
  Pending: "Inasubiri",
  "Publish these records at your DNS provider, then use Check now on this page. Mail hostnames must be DNS only, never proxied.":
    "Chapisha rekodi hizi kwa mtoa huduma wako wa DNS, kisha tumia Kagua sasa kwenye ukurasa huu. Majina ya wenyeji wa barua lazima yawe DNS pekee, kamwe yasipitishwe kwenye proksi.",
  "Publishing a DKIM record does not prove your mail is signed. Send a test message to your own mailbox and use Proof that mail is signed on this page before sending real mail.":
    "Kuchapisha rekodi ya DKIM hakuthibitishi kuwa barua yako imetiwa saini. Tuma ujumbe wa majaribio kwenye kisanduku chako na utumie Uthibitisho kuwa barua imetiwa saini kwenye ukurasa huu kabla ya kutuma barua halisi.",
  Ready: "Tayari",
  Record: "Rekodi",
  "Records written. Now checking DNS.": "Rekodi zimeandikwa. Sasa inakagua DNS.",
  "Replace token": "Badilisha tokeni",
  Result: "Matokeo",
  Review: "Pitia",
  "Roll back": "Rudisha nyuma",
  "Roll back the records Njiwa-Mail added?": "Kurudisha nyuma rekodi ambazo Njiwa-Mail iliongeza?",
  "Rollback finished.": "Kurudisha nyuma kumekamilika.",
  "Save token": "Hifadhi tokeni",
  "Select a zone": "Chagua eneo",
  "Select zone": "Chagua eneo",
  "Set up DNS automatically with Cloudflare": "Sanidi DNS kiotomatiki kwa Cloudflare",
  "Sign in with Cloudflare": "Ingia kwa Cloudflare",
  "Template, not a record to copy: replace the part in <…> with your mail server's details before publishing.":
    "Kiolezo, si rekodi ya kunakili: badilisha sehemu iliyo ndani ya <…> kwa maelezo ya seva yako ya barua kabla ya kuchapisha.",
  "The approval expires after 30 minutes. Applying writes the records you reviewed, and nothing else.":
    "Idhini huisha baada ya dakika 30. Kutekeleza huandika rekodi ulizozipitia, na si kingine.",
  "The records you approved will be written to Cloudflare. If DNS changed since you reviewed it, nothing is written.":
    "Rekodi ulizoidhinisha zitaandikwa kwenye Cloudflare. Ikiwa DNS imebadilika tangu ulipopitia, hakuna kinachoandikwa.",
  "The saved credential is deleted. DNS records that were already added stay exactly as they are; they are not removed.":
    "Kitambulisho kilichohifadhiwa kinafutwa. Rekodi za DNS zilizokwisha ongezwa hubaki kama zilivyo; hazifutwi.",
  "There is nothing to write. Resolve the items above or verify the domain.":
    "Hakuna cha kuandika. Tatua vipengele vilivyo juu au thibitisha kikoa.",
  "This connection cannot be used any more. Connect again below.":
    "Muunganisho huu hauwezi kutumika tena. Unganisha tena hapa chini.",
  "This credential cannot see any zone. Check the token's zone permissions.":
    "Kitambulisho hiki hakioni eneo lolote. Angalia ruhusa za maeneo za tokeni.",
  "Token replaced.": "Tokeni imebadilishwa.",
  "Use an API token": "Tumia tokeni ya API",
  Verified: "Imethibitishwa",
  Verify: "Thibitisha",
  Warnings: "Maonyo",
  "Will be changed": "Itabadilishwa",
  "Will be created": "Itaundwa",
  "You approve access on Cloudflare's own page. Njiwa-Mail never sees your password.":
    "Unaidhinisha ufikiaji kwenye ukurasa wa Cloudflare wenyewe. Njiwa-Mail haioni nenosiri lako kamwe.",
  "Your existing record is edited in place. It keeps its TTL and everything else in it.":
    "Rekodi yako iliyopo inahaririwa hapohapo. Inabaki na TTL yake na kila kitu kingine ndani yake.",
  Zone: "Eneo",
  "affects existing setup": "inaathiri usanidi uliopo",
  "already present; left as it was": "tayari ipo; imeachwa kama ilivyokuwa",
  created: "imeundwa",
  done: "imekamilika",
  edited: "imehaririwa",
  "signed in": "imeingia kwa akaunti",
  "zone(s) visible": "eneo/maeneo yanayoonekana",
};
