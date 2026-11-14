import { swDns } from "./sw-dns";
import { swSetup } from "./sw-setup";
// Kiswahili for the strings that predate the installation screens. Keys are the exact English text passed to phrase().
export const swExtra: Record<string, string> = {
  "A CSV with a header row, or a JSON array of objects. Check first: nothing is saved until you import.":
    "CSV yenye safu ya vichwa, au JSON yenye orodha ya vitu. Kagua kwanza: hakuna kinachohifadhiwa hadi uingize.",
  "Add condition": "Ongeza sharti",
  "Add contact": "Ongeza mwasiliani",
  "Add everyone to list": "Ongeza wote kwenye orodha",
  "Add only people who agreed to receive your marketing mail.":
    "Ongeza tu watu waliokubali kupokea barua zako za masoko.",
  "Add to list": "Ongeza kwenye orodha",
  "All conditions": "Masharti yote",
  "Any condition": "Sharti lolote",
  Attribute: "Sifa",
  "Attributes are for segments, such as a plan or a city. Do not store secrets in them.":
    "Sifa ni za kugawa makundi, kama mpango au mji. Usihifadhi siri ndani yake.",
  Attributes: "Sifa",
  "Audience:": "Walengwa:",
  "Audiences from rules, worked out again each time a campaign is created.":
    "Walengwa kutokana na kanuni, wanahesabiwa upya kila kampeni inapoundwa.",
  "Campaigns already created keep their recipients.":
    "Kampeni zilizokwisha undwa zinabaki na wapokeaji wao.",
  Check: "Kagua",
  "Choose a file": "Chagua faili",
  "Choose a list.": "Chagua orodha.",
  "Choose a segment.": "Chagua kundi.",
  Clicks: "Mibofyo",
  "Contact deleted.": "Mwasiliani amefutwa.",
  Contacts: "Waasiliani",
  "Count link clicks": "Hesabu mibofyo ya viungo",
  "Count opens (an estimate: some mail apps load images on their own)":
    "Hesabu ufunguzi (makadirio: baadhi ya programu za barua hupakia picha zenyewe)",
  "Create a list, then import contacts into it.":
    "Unda orodha, kisha ingiza waasiliani ndani yake.",
  "Delete this contact?": "Futa mwasiliani huyu?",
  Detect: "Tambua",
  "Email address": "Anwani ya barua pepe",
  "Email column": "Safu ya barua pepe",
  Field: "Sehemu",
  "Find by exact address": "Tafuta kwa anwani kamili",
  Find: "Tafuta",
  "First row:": "Safu ya kwanza:",
  Format: "Muundo",
  "Import a CSV or JSON file, add someone, or let your application add sign-ups with the API.":
    "Ingiza faili la CSV au JSON, ongeza mtu, au acha programu yako iongeze wanaojisajili kupitia API.",
  "Import contacts": "Ingiza waasiliani",
  Import: "Ingiza",
  "Last message:": "Ujumbe wa mwisho:",
  Link: "Kiungo",
  "List created.": "Orodha imeundwa.",
  "List deleted.": "Orodha imefutwa.",
  List: "Orodha",
  Lists: "Orodha",
  Match: "Linganisha",
  Members: "Wanachama",
  "Messages to this address": "Ujumbe kwa anwani hii",
  "Name column": "Safu ya jina",
  "New list": "Orodha mpya",
  "New segment": "Kundi jipya",
  Newsletter: "Jarida",
  "No contacts": "Hakuna waasiliani",
  "No lists": "Hakuna orodha",
  "Not on any list.": "Hayupo kwenye orodha yoyote.",
  "Only contacts who can receive marketing mail are included. Their name and attributes are the variables.":
    "Ni waasiliani wanaoweza kupokea barua za masoko tu wanaojumuishwa. Jina na sifa zao ndizo vigezo.",
  "Opens and clicks": "Ufunguzi na mibofyo",
  "Opens are an estimate: some mail apps load images on their own, others block them.":
    "Ufunguzi ni makadirio: baadhi ya programu za barua hupakia picha zenyewe, nyingine huzizuia.",
  "People who receive marketing mail. A bounce, a complaint or an unsubscribe is shown here and always respected.":
    "Watu wanaopokea barua za masoko. Barua iliyorudi, malalamiko au kujiondoa huonyeshwa hapa na huheshimiwa daima.",
  "Remove condition": "Ondoa sharti",
  "Resubscribe to all marketing mail": "Jiunge tena na barua zote za masoko",
  Resubscribe: "Jiunge tena",
  "Resubscribed.": "Amejiunga tena.",
  Row: "Safu",
  "Save segment": "Hifadhi kundi",
  "Saved segments": "Makundi yaliyohifadhiwa",
  "Segment deleted.": "Kundi limefutwa.",
  Segment: "Kundi",
  Segments: "Makundi",
  "Subscriber lists. Unsubscribing from a list's mail leaves that list only, unless the reader chooses all marketing mail.":
    "Orodha za wanaojiunga. Kujiondoa kwenye barua za orodha kunamtoa kwenye orodha hiyo tu, isipokuwa msomaji achague barua zote za masoko.",
  "The contacts stay; only the list and its memberships are deleted.":
    "Waasiliani wanabaki; ni orodha na uanachama wake tu vinavyofutwa.",
  "Their list memberships go too. Suppressions and unsubscribes stay.":
    "Uanachama wake wa orodha unaondoka pia. Vizuizi na kujiondoa vinabaki.",
  "Tracking (off by default)": "Ufuatiliaji (umezimwa kwa chaguo-msingi)",
  "Unique clicks": "Mibofyo ya kipekee",
  "Unique opens (estimate)": "Ufunguzi wa kipekee (makadirio)",
  Unsubscribed: "Wamejiondoa",
  "Update contacts that already exist": "Sasisha waasiliani waliopo tayari",
  added: "wameongezwa",
  "already there": "walikuwepo tayari",
  "can receive marketing mail now": "wanaweza kupokea barua za masoko sasa",
  "contacts match": "waasiliani wanalingana",
  list: "orodha",
  "not imported": "hawakuingizwa",
  rows: "safu",
  segment: "kundi",
  unsubscribed: "amejiondoa",
  updated: "wamesasishwa",
  Audience: "Walengwa",
  "Paste or upload": "Bandika au pakia",
  "A list": "Orodha",
  "A segment": "Kundi",
  bounced: "barua zimerudi",
  complained: "amelalamika",
  status: "hali",
  domain: "kikoa",
  attribute: "sifa",
  created: "aliongezwa",
  eq: "ni sawa na",
  neq: "si sawa na",
  in: "yumo kwenye",
  "not in": "hayumo kwenye",
  contains: "ina",
  exists: "ipo",
  "not exists": "haipo",
  after: "baada ya",
  before: "kabla ya",
  "Receive over HTTP": "Pokea kupitia HTTP",
  "New secret": "Siri mpya",
  "New secret created.": "Siri mpya imeundwa.",
  "Turn off HTTP ingest?": "Zima upokeaji kupitia HTTP?",
  "Signed requests are refused until you create a new secret.":
    "Maombi yaliyosainiwa yatakataliwa hadi uunde siri mpya.",
  "Turn off": "Zima",
  "HTTP ingest turned off.": "Upokeaji kupitia HTTP umezimwa.",
  "Turn on HTTP ingest": "Washa upokeaji kupitia HTTP",
  "A server or provider of yours can pass each received message to these mailboxes over HTTP, signed with a secret.":
    "Seva au mtoa huduma wako anaweza kuleta kila ujumbe uliopokelewa kwenye masanduku haya kupitia HTTP, ukiwa umesainiwa kwa siri.",
  Endpoint: "Anwani ya kupokea",
  "Last used": "Ilitumika mwisho",
  "Copy this secret now. It is not shown again.": "Nakili siri hii sasa. Haitaonyeshwa tena.",
  "(no HTML part)": "(hakuna sehemu ya HTML)",
  "(no subject)": "(hakuna mada)",
  "(no text part)": "(hakuna sehemu ya maandishi)",
  "(tick “Relay all recipients”)": "(weka tiki kwenye “Relay all recipients”)",
  ", then publish the key as the DKIM record above.":
    ", kisha chapisha ufunguo kama rekodi ya DKIM iliyo juu.",
  "1 day": "siku 1",
  "1 hour": "saa 1",
  "1 to 10000 per minute.": "1 hadi 10000 kwa dakika.",
  "12+ characters; share it privately.": "Herufi 12 au zaidi; mshirikishe kwa siri.",
  "6 digits from the app, or a recovery code such as ABCDE-FGHIJ.":
    "Tarakimu 6 kutoka kwenye programu, au msimbo wa kurejesha kama ABCDE-FGHIJ.",
  "7 days": "siku 7",
  "A dedicated mailbox, for example njiwa@example.com. Not a personal or admin account.":
    "Kisanduku maalum cha barua, kwa mfano njiwa@example.com. Si akaunti ya kibinafsi au ya msimamizi.",
  "A domain is claimed when a project's ownership check passes.":
    "Kikoa hudaiwa pale ukaguzi wa umiliki wa mradi unapofaulu.",
  "A key is stored. Leave empty to keep it.":
    "Ufunguo umehifadhiwa. Acha wazi ili uendelee kutumia huo.",
  "A new key is created and shown once. The old key keeps working for 24 hours so you can switch over, then stops.":
    "Ufunguo mpya huundwa na kuonyeshwa mara moja tu. Ufunguo wa zamani unaendelea kufanya kazi kwa saa 24 ili ubadilishe, kisha huacha.",
  "A new message is queued with the same content. The first one ended without being accepted.":
    "Ujumbe mpya unawekwa kwenye foleni wenye maudhui yaleyale. Wa kwanza uliisha bila kukubaliwa.",
  "A number, usually 587 or 465.": "Nambari, kwa kawaida 587 au 465.",
  "A password is stored. Leave empty to keep it.":
    "Nenosiri limehifadhiwa. Acha wazi ili uendelee kutumia hilo.",
  "A plain-text version is generated automatically.":
    "Toleo la maandishi matupu huundwa kiotomatiki.",
  "A published DKIM record does not prove your mail is signed. Send a test to your own mailbox, open the original message (Show original / View source) and paste it here.":
    "Rekodi ya DKIM iliyochapishwa haithibitishi kuwa barua yako imetiwa saini. Tuma jaribio kwenye kisanduku chako, fungua ujumbe halisi (Show original / View source) na ubandike hapa.",
  "A reason is required.": "Sababu inahitajika.",
  "A received message carried a valid DKIM signature for this domain":
    "Ujumbe uliopokelewa ulikuwa na saini halali ya DKIM kwa kikoa hiki",
  "A subdomain such as notify.example.com keeps mail separate from your main domain.":
    "Kikoa kidogo kama notify.example.com hutenganisha barua na kikoa chako kikuu.",
  "API URL": "URL ya API",
  "API host": "Mwenyeji wa API",
  "API key": "Ufunguo wa API",
  "API quick start": "Mwanzo wa haraka wa API",
  "Above this project's limit of": "Juu ya kikomo cha mradi huu cha",
  Accepted: "Imekubaliwa",
  "Accepted by the mail server (submitted)": "Imekubaliwa na seva ya barua (imewasilishwa)",
  Acknowledge: "Tambua",
  Acknowledged: "Imetambuliwa",
  "Acknowledged.": "Imetambuliwa.",
  Action: "Kitendo",
  Active: "Inatumika",
  Actor: "Mhusika",
  Add: "Ongeza",
  "Add a bulk domain": "Ongeza kikoa cha barua nyingi",
  "Add a domain first": "Ongeza kikoa kwanza",
  "Add a second step with an authenticator app (Google Authenticator, Aegis, 1Password, …).":
    "Ongeza hatua ya pili kwa programu ya uthibitisho (Google Authenticator, Aegis, 1Password, …).",
  "Add a sender first.": "Ongeza mtumaji kwanza.",
  "Add a verified sender first": "Ongeza mtumaji aliyethibitishwa kwanza",
  "Add a webhook endpoint first": "Ongeza endpoint ya webhook kwanza",
  "Add an SMTP profile": "Ongeza wasifu wa SMTP",
  "Add endpoint": "Ongeza endpoint",
  "Add profile": "Ongeza wasifu",
  "Add recipients.": "Ongeza wapokeaji.",
  "Add sender": "Ongeza mtumaji",
  "Add the address your mail comes from, such as hello@ or no-reply@.":
    "Ongeza anwani ambayo barua yako inatoka, kama hello@ au no-reply@.",
  "Add the domain you will send from. You will get the exact DNS records to publish.":
    "Ongeza kikoa utakachotumia kutuma. Utapata rekodi kamili za DNS za kuchapisha.",
  "Add this domain to Mailcow, create its DKIM key and (for receiving) the relay transport, through the Mailcow API.":
    "Ongeza kikoa hiki kwenye Mailcow, unda ufunguo wake wa DKIM na (kwa kupokea) usafirishaji wa relay, kupitia API ya Mailcow.",
  "Add user": "Ongeza mtumiaji",
  "Add variable": "Ongeza kigezo",
  Added: "Imeongezwa",
  "Added by": "Ameongeza",
  "Added by hand": "Imeongezwa kwa mkono",
  "Adding…": "Inaongeza…",
  "Address suppressed.": "Anwani imezuiwa.",
  "Addresses that receive mail into the Inbox.":
    "Anwani zinazopokea barua kwenye Sanduku la barua.",
  "Addresses this environment may send from. Compose and Bulk send preselect the default one.":
    "Anwani ambazo mazingira haya yanaweza kutuma kutoka. Compose na Bulk send huchagua ile chaguomsingi.",
  "Addresses this environment will not send to. Bulk unsubscribes affect announcements only.":
    "Anwani ambazo mazingira haya hayatatuma kwake. Kujiondoa kwenye Bulk kunaathiri matangazo pekee.",
  "All senders": "Watumaji wote",
  "All sources": "Vyanzo vyote",
  "All states": "Hali zote",
  "Allow the SMTP login to send as addresses on this domain, and have it sign them with DKIM. Publish the DKIM key your mail server gives you as the DKIM record above.":
    "Ruhusu kuingia kwa SMTP kutuma kama anwani za kikoa hiki, na kuzitia saini kwa DKIM. Chapisha ufunguo wa DKIM ambao seva yako ya barua inakupa kama rekodi ya DKIM iliyo juu.",
  "An address the login may send as; used by the sender check and test email.":
    "Anwani ambayo kuingia kunaweza kutuma kama; hutumika na ukaguzi wa mtumaji na barua pepe ya jaribio.",
  "An email address.": "Anwani ya barua pepe.",
  Announcement: "Tangazo",
  "Applications and Compose can no longer send from this address in this environment. Sent mail is kept.":
    "Programu na Compose haziwezi tena kutuma kutoka anwani hii kwenye mazingira haya. Barua zilizotumwa zinahifadhiwa.",
  "Applications of this project can no longer send. Queued mail stays queued.":
    "Programu za mradi huu haziwezi tena kutuma. Barua zilizo kwenye foleni zinabaki kwenye foleni.",
  "Applications send through the /v1 API with an API key of this project and environment.":
    "Programu hutuma kupitia API ya /v1 kwa ufunguo wa API wa mradi na mazingira haya.",
  "Applications using this key stop sending at once. This cannot be undone.":
    "Programu zinazotumia ufunguo huu zinaacha kutuma mara moja. Hili haliwezi kutenduliwa.",
  "Approved by": "Imeidhinishwa na",
  "Archived.": "Imehifadhiwa kwenye kumbukumbu.",
  "Asked not to be contacted": "Aliomba asiwasiliane naye",
  "At most": "Kiwango cha juu",
  "At most 40 characters.": "Herufi 40 kwa juu.",
  "Attach files": "Ambatisha faili",
  "Attachments may total at most 5 MB.": "Viambatisho vinaweza kuwa jumla ya MB 5 kwa juu.",
  Automation: "Otomatiki",
  "Back to Domains": "Rudi kwenye Domains",
  "Block this sender?": "Kumzuia mtumaji huyu?",
  Blocked: "Imezuiwa",
  "Bounce address": "Anwani ya bounce",
  "Bounces and complaints": "Bounce na malalamiko",
  Breadcrumb: "Njia ya urambazaji",
  "Bulk mail goes from its own verified subdomain, carries an unsubscribe link, and is paced behind transactional mail.":
    "Barua nyingi hutoka kwenye kikoa kidogo chake kilichothibitishwa, huwa na kiungo cha kujiondoa, na hupewa kasi nyuma ya barua za miamala.",
  "Bulk mail must come from its own subdomain, such as news.yourdomain, so problems there do not hurt transactional mail. Add that domain with “Use only for bulk announcements”, verify it, then add a sender on it.":
    "Barua nyingi lazima zitoke kwenye kikoa kidogo chake, kama news.yourdomain, ili matatizo huko yasiathiri barua za miamala. Ongeza kikoa hicho kwa “Use only for bulk announcements”, kithibitishe, kisha ongeza mtumaji juu yake.",
  "Bulk mail waits behind transactional mail.": "Barua nyingi husubiri nyuma ya barua za miamala.",
  "Bulk send requires its own subdomain (for example news.example.com) so problems there never affect transactional mail.":
    "Bulk send inahitaji kikoa kidogo chake (kwa mfano news.example.com) ili matatizo huko yasiathiri kamwe barua za miamala.",
  "Cancel this announcement?": "Kughairi tangazo hili?",
  "Cancel this message?": "Kughairi ujumbe huu?",
  "Cancel this scheduled message?": "Kughairi ujumbe huu ulioratibiwa?",
  "Cancelled.": "Imeghairiwa.",
  Captured: "Imenaswa",
  "Captured HTML (sandboxed)": "HTML iliyonaswa (kwenye sandbox)",
  "Catch-all: receive mail for every address on this domain not claimed by another mailbox":
    "Catch-all: pokea barua za kila anwani kwenye kikoa hiki ambazo hazijadaiwa na kisanduku kingine",
  Cause: "Sababu",
  "Changing mode can affect queued messages; it goes through a planned switch, not this page.":
    "Kubadilisha hali kunaweza kuathiri ujumbe ulio kwenye foleni; hufanyika kupitia mabadiliko yaliyopangwa, si ukurasa huu.",
  "Channel added.": "Chaneli imeongezwa.",
  "Check recipients": "Kagua wapokeaji",
  "Check signature": "Kagua saini",
  "Checking…": "Inakagua…",
  "Choose a message": "Chagua ujumbe",
  "Choose a sender.": "Chagua mtumaji.",
  "Choose a template.": "Chagua kiolezo.",
  "Choose at least one event.": "Chagua angalau tukio moja.",
  "Choose at least one permission.": "Chagua angalau ruhusa moja.",
  "Choose…": "Chagua…",
  "Claimed domains": "Vikoa vilivyodaiwa",
  Code: "Msimbo",
  "Common DNS providers": "Watoa huduma wa kawaida wa DNS",
  Complaint: "Malalamiko",
  "Complaints arrive when providers send feedback-loop reports to fbl@ on the bounce domain (see Mail server setup).":
    "Malalamiko huja watoa huduma wanapotuma ripoti za feedback-loop kwa fbl@ kwenye kikoa cha bounce (angalia usanidi wa Seva ya barua).",
  "Compose a test": "Tunga jaribio",
  "Configuration → ARC/DKIM keys → add a 2048-bit key for":
    "Configuration → ARC/DKIM keys → ongeza ufunguo wa biti 2048 kwa",
  "Configuration → Mail setup → Domains → Add domain:":
    "Configuration → Mail setup → Domains → Add domain:",
  "Configuration → Routing → Transport maps → add destination":
    "Configuration → Routing → Transport maps → ongeza lengo",
  "Configure the Mailcow API on the Mail server page":
    "Sanidi API ya Mailcow kwenye ukurasa wa Seva ya barua",
  "Connecting the mail server is done from the Mail server page inside the project; you can do it first.":
    "Kuunganisha seva ya barua hufanyika kutoka ukurasa wa Seva ya barua ndani ya mradi; unaweza kuanza nao.",
  "Connection test failed": "Jaribio la muunganisho limeshindwa",
  "Connection test passed": "Jaribio la muunganisho limefaulu",
  "Connection test recorded.": "Jaribio la muunganisho limerekodiwa.",
  Content: "Maudhui",
  "Create address": "Unda anwani",
  "Create an address such as help@ to receive replies and support mail.":
    "Unda anwani kama help@ kupokea majibu na barua za msaada.",
  "Create key": "Unda ufunguo",
  "Create one for each application that sends mail.":
    "Unda mmoja kwa kila programu inayotuma barua.",
  "Creating…": "Inaunda…",
  "DKIM public key": "Ufunguo wa umma wa DKIM",
  "DKIM selector": "Selector ya DKIM",
  "DKIM signer": "Kitia saini cha DKIM",
  "DNS records": "Rekodi za DNS",
  "Daily totals for": "Jumla za kila siku za",
  "Default sender set.": "Chaguomsingi la mtumaji limewekwa.",
  "Delete all stored data for": "Futa data yote iliyohifadhiwa ya",
  "Delete draft": "Futa rasimu",
  "Delete from queue": "Futa kutoka kwenye foleni",
  "Delete rule": "Futa kanuni",
  "Delete this domain?": "Kufuta kikoa hiki?",
  "Delete this endpoint?": "Kufuta endpoint hii?",
  "Delete this message from the mail queue?": "Kufuta ujumbe huu kutoka kwenye foleni ya barua?",
  "Delete this message?": "Kufuta ujumbe huu?",
  "Delete this temporary inbox?": "Kufuta kisanduku hiki cha muda?",
  "Deleted from the mail queue.": "Imefutwa kwenye foleni ya barua.",
  "Deleted.": "Imefutwa.",
  Deliveries: "Uwasilishaji",
  "Delivery queued again.": "Uwasilishaji umewekwa kwenye foleni tena.",
  "Delivery settings changed since the last check, so these are the records needed now. Publish any that changed, then run Check now.":
    "Mipangilio ya utumaji ilibadilika tangu ukaguzi wa mwisho, kwa hiyo hizi ni rekodi zinazohitajika sasa. Chapisha zilizobadilika, kisha endesha Check now.",
  Details: "Maelezo",
  Diagnostic: "Uchunguzi",
  Disable: "Zima",
  "Disabled.": "Imezimwa.",
  "Display name (optional)": "Jina la kuonyesha (si lazima)",
  "Disposable addresses for testing sign-up and password-reset flows. The address and its mail are deleted when it expires.":
    "Anwani za kutupwa za kujaribu usajili na mtiririko wa kuweka upya nenosiri. Anwani na barua zake hufutwa inapoisha muda.",
  "Domain deleted.": "Kikoa kimefutwa.",
  "Domain ownership": "Umiliki wa kikoa",
  "Domains this project sends from or receives on.":
    "Vikoa ambavyo mradi huu hutuma kutoka au hupokea juu yake.",
  Done: "Imekamilika",
  "Draft saved.": "Rasimu imehifadhiwa.",
  "Each project has its own domains, senders, keys and mail, in a Sandbox and a Live environment.":
    "Kila mradi una vikoa, watumaji, funguo na barua zake, kwenye mazingira ya Sandbox na Live.",
  "Each sending domain belongs to one project in this installation: the first whose ownership check passed. Other projects cannot send from it until an operator transfers it.":
    "Kila kikoa cha kutuma ni cha mradi mmoja katika usakinishaji huu: wa kwanza ambao ukaguzi wake wa umiliki ulifaulu. Miradi mingine haiwezi kutuma kutoka huko hadi mwendeshaji akihamishe.",
  Edit: "Hariri",
  "Edit rule": "Hariri kanuni",
  Enable: "Washa",
  "Enabled.": "Imewashwa.",
  Encryption: "Usimbaji fiche",
  "Endpoint URL": "URL ya endpoint",
  "Endpoint added.": "Endpoint imeongezwa.",
  "Endpoint deleted.": "Endpoint imefutwa.",
  "Enter a domain such as notify.example.com": "Weka kikoa kama notify.example.com",
  "Enter a subject.": "Weka mada.",
  "Enter an email address.": "Weka anwani ya barua pepe.",
  "Enter one email address.": "Weka anwani moja ya barua pepe.",
  "Enter the part before @.": "Weka sehemu iliyo kabla ya @.",
  Environments: "Mazingira",
  Event: "Tukio",
  Events: "Matukio",
  "Every change made by a person or an API key.":
    "Kila badiliko lililofanywa na mtu au ufunguo wa API.",
  "Every message of this project and environment, from every source. Metadata only.":
    "Kila ujumbe wa mradi na mazingira haya, kutoka kila chanzo. Metadata pekee.",
  "Every new key is published. You can activate on the host: njiwa-mail installer dkim-rotate activate.":
    "Kila ufunguo mpya umechapishwa. Unaweza kuwasha kwenye seva: njiwa-mail installer dkim-rotate activate.",
  "Every project's ownership proof for it is reset: the new owner must check its DNS again, and the previous owner's senders stop being verified at once.":
    "Uthibitisho wa umiliki wa kila mradi kwa kikoa hiki unawekwa upya: mmiliki mpya lazima akague DNS yake tena, na watumaji wa mmiliki wa awali wanaacha kuthibitishwa mara moja.",
  "Everything the mail server accepted has been handed on.":
    "Kila kitu ambacho seva ya barua ilikubali kimekabidhiwa mbele.",
  Evidence: "Ushahidi",
  "Evidence comes from the mail server log (njiwa-mail mta-log) and from bounce reports. Silence only means nothing arrived: with no mail sent there are no events.":
    "Ushahidi hutoka kwenye kumbukumbu za seva ya barua (njiwa-mail mta-log) na kwenye ripoti za bounce. Ukimya unamaanisha tu hakuna kilichofika: bila barua zilizotumwa hakuna matukio.",
  "Evidence shows what servers reported. No server can confirm that a message reached the inbox.":
    "Ushahidi unaonyesha kile seva ziliripoti. Hakuna seva inayoweza kuthibitisha kuwa ujumbe ulifika kwenye sanduku la barua.",
  "Exactly one profile sends Live mail. Switching is planned: pending mail is reviewed first, and messages with an unknown outcome are never moved.":
    "Wasifu mmoja tu hutuma barua za Live. Kubadilisha hupangwa: barua zinazosubiri hukaguliwa kwanza, na ujumbe usio na matokeo yanayojulikana haujawahi kuhamishwa.",
  Expires: "Inaisha",
  Export: "Pakua",
  "Export or delete one recipient's stored data in":
    "Pakua au futa data iliyohifadhiwa ya mpokeaji mmoja ndani ya",
  "Failed deliveries": "Uwasilishaji ulioshindwa",
  Filter: "Chuja",
  "Filtered by": "Imechujwa kwa",
  "Find the message in the mail server log first. This records what happened; it does not send again.":
    "Tafuta ujumbe kwenye kumbukumbu za seva ya barua kwanza. Hii inarekodi kilichotokea; haitumi tena.",
  "For example: auto-reply to mail at help@ with a “we received your message” template.":
    "Kwa mfano: jibu la kiotomatiki kwa barua za help@ lenye kiolezo cha “tumepokea ujumbe wako”.",
  "For receiving, have your mail server relay this domain's mail to the Njiwa-Mail inbound listener.":
    "Kwa kupokea, mwambie seva yako ya barua isafirishe barua za kikoa hiki kwenda kwa inbound listener ya Njiwa-Mail.",
  Found: "Kilichopatikana",
  "From announcement": "Kutoka kwenye tangazo",
  "From date": "Kuanzia tarehe",
  "Generic works with any SMTP server. Mailcow only adds optional helpers; it is never required to send.":
    "Generic hufanya kazi na seva yoyote ya SMTP. Mailcow huongeza tu visaidizi vya hiari; haihitajiki kamwe kutuma.",
  "Give a reason.": "Toa sababu.",
  "Give it a name.": "Ipe jina.",
  "Go to Sender addresses →": "Nenda kwenye Sender addresses →",
  "HTML body": "Mwili wa HTML",
  "HTML preview": "Hakikisho la HTML",
  "Handed over in 24 hours": "Zilizokabidhiwa ndani ya saa 24",
  "Hard bounce": "Hard bounce",
  "Hard bounces": "Hard bounce",
  "Hard bounces and complaints are added automatically.":
    "Hard bounce na malalamiko huongezwa kiotomatiki.",
  "Has an attachment": "Ina kiambatisho",
  "Held.": "Imeshikiliwa.",
  "Help with this record": "Msaada kuhusu rekodi hii",
  Hide: "Ficha",
  "Hide headers": "Ficha vichwa",
  Host: "Mwenyeji",
  "How this installation hands mail to the internet, and whether it is ready. These settings apply to every project.":
    "Jinsi usakinishaji huu unavyokabidhi barua kwenye intaneti, na kama uko tayari. Mipangilio hii inatumika kwa kila mradi.",
  "How to publish these": "Jinsi ya kuchapisha hizi",
  "In 30 days": "Ndani ya siku 30",
  "In 90 days": "Ndani ya siku 90",
  "In Live only a published version can be tested, and only to an address on this environment's approved list.":
    "Kwenye Live, toleo lililochapishwa pekee linaweza kujaribiwa, na kwa anwani iliyo kwenye orodha iliyoidhinishwa ya mazingira haya tu.",
  "In Sandbox the test is captured, never delivered; drafts can be tested.":
    "Kwenye Sandbox jaribio hunaswa, halitumwi kamwe; rasimu zinaweza kujaribiwa.",
  "In one year": "Ndani ya mwaka mmoja",
  "In the queue": "Kwenye foleni",
  "In your authenticator app add an account with this key (time based):":
    "Kwenye programu yako ya uthibitisho ongeza akaunti kwa ufunguo huu (kulingana na muda):",
  "Inbound domain": "Kikoa cha inbound",
  "Inbound listener address": "Anwani ya inbound listener",
  "Inbound listener reachable": "Inbound listener inafikika",
  "Insert variables as {{.name}}.": "Weka vigezo kama {{.name}}.",
  "It is hidden and its stored passwords are deleted. Messages already sent through it keep their history.":
    "Imefichwa na nywila zake zilizohifadhiwa zinafutwa. Ujumbe uliokwisha tumwa kupitia hiyo unabaki na historia yake.",
  "It is used by": "Inatumiwa na",
  "It will never be delivered. Njiwa-Mail keeps the message record and notes the deletion on its timeline. This cannot be undone.":
    "Haitatumwa kamwe. Njiwa-Mail inahifadhi rekodi ya ujumbe na kuandika ufutaji kwenye ratiba yake. Hili haliwezi kutenduliwa.",
  "It will not be sent to": "Haitatumwa kwa",
  "It will not be sent. This cannot be undone.": "Haitatumwa. Hili haliwezi kutenduliwa.",
  "Its DNS check history stays; live sending from it stops.":
    "Historia ya ukaguzi wake wa DNS inabaki; utumaji wa moja kwa moja kutoka kwake unaacha.",
  Key: "Ufunguo",
  "Key revoked.": "Ufunguo umebatilishwa.",
  "Key rotated.": "Ufunguo umebadilishwa.",
  "Key saved. Run Check now to compare it with DNS.":
    "Ufunguo umehifadhiwa. Endesha Check now kuulinganisha na DNS.",
  "Keys for applications to send from this project's":
    "Funguo za programu kutuma kutoka kwenye mradi huu",
  Language: "Lugha",
  "Last bounce report": "Ripoti ya mwisho ya bounce",
  "Last handed to the mail server": "Ilikabidhiwa mwisho kwa seva ya barua",
  "Last inbound message": "Ujumbe wa mwisho uliopokelewa",
  "Last received": "Ilipokelewa mwisho",
  "Last response": "Jibu la mwisho",
  "Last sign-in": "Kuingia kwa mwisho",
  "Leave empty to keep the stored password":
    "Acha wazi ili uendelee kutumia nenosiri lililohifadhiwa",
  "Letters, digits, dashes or underscores. Used in links.":
    "Herufi, tarakimu, vistari au mistari chini. Hutumika kwenye viungo.",
  Lifetime: "Muda wa kuishi",
  Limits: "Vikomo",
  "Links (url type) must start with one of this environment's approved origins.":
    "Viungo (aina ya url) lazima vianze na mojawapo ya asili zilizoidhinishwa za mazingira haya.",
  "Live mail in this project.": "Barua za Live katika mradi huu.",
  "Live mail will go out through this profile from now on. The current profile is kept, and you can switch back the same way.":
    "Barua za Live zitatoka kupitia wasifu huu kuanzia sasa. Wasifu wa sasa unahifadhiwa, na unaweza kurudi kwa njia hiyohiyo.",
  "Live: the message goes to real recipients.": "Live: ujumbe huenda kwa wapokeaji halisi.",
  "Lowercase letters, digits and dashes.": "Herufi ndogo, tarakimu na vistari.",
  "Mail can only be sent from an address on a verified domain of this project.":
    "Barua inaweza kutumwa tu kutoka anwani iliyo kwenye kikoa kilichothibitishwa cha mradi huu.",
  "Mail delivery profiles": "Wasifu wa utumaji barua",
  "Mail delivery switched.": "Utumaji wa barua umebadilishwa.",
  "Mail from": "Barua kutoka",
  "Mail leaves with a bounce address on this domain so SPF aligns with your From domain for DMARC. Leave it empty to use the installation's bounce domain, which works when it shares your organisation's domain. It must be this domain or a subdomain, for example":
    "Barua hutoka na anwani ya bounce kwenye kikoa hiki ili SPF ilingane na kikoa chako cha From kwa DMARC. Acha wazi kutumia kikoa cha bounce cha usakinishaji, kinachofaa kikiwa na kikoa kimoja cha shirika lako. Lazima kiwe kikoa hiki au kikoa kidogo chake, kwa mfano",
  "Mail received by this project's mailboxes.": "Barua zilizopokelewa na visanduku vya mradi huu.",
  "Mail sent in the Sandbox environment is captured here instead of being delivered. Captures expire after a day.":
    "Barua zilizotumwa kwenye mazingira ya Sandbox hunaswa hapa badala ya kuwasilishwa. Zilizonaswa huisha baada ya siku moja.",
  "Mail server reply": "Jibu la seva ya barua",
  "Mail server settings saved. Run the connection test.":
    "Mipangilio ya seva ya barua imehifadhiwa. Endesha jaribio la muunganisho.",
  "Mail server →": "Seva ya barua →",
  "Mail to this address appears in the Inbox.":
    "Barua kwa anwani hii huonekana kwenye Sanduku la barua.",
  "Mail to this address will be sent again. Only do this if the reason no longer applies.":
    "Barua kwa anwani hii zitatumwa tena. Fanya hivi tu kama sababu haipo tena.",
  "Mail was handed over but no log evidence arrived. If this persists, check that njiwa-mail-mta-log is running.":
    "Barua ilikabidhiwa lakini hakuna ushahidi wa kumbukumbu uliofika. Hali ikiendelea, hakikisha njiwa-mail-mta-log inafanya kazi.",
  "Mailbox deleted.": "Kisanduku kimefutwa.",
  Mailcow: "Mailcow",
  "Mailcow API (optional)": "API ya Mailcow (hiari)",
  "Mailcow API reachable": "API ya Mailcow inafikika",
  "Mailcow operator steps (docs/MAILCOW_SETUP.md)":
    "Hatua za mwendeshaji wa Mailcow (docs/MAILCOW_SETUP.md)",
  "Main navigation": "Urambazaji mkuu",
  "Make an installation operator:": "Mfanye mwendeshaji wa usakinishaji:",
  "Make default": "Fanya chaguomsingi",
  Manage: "Simamia",
  "Marked as unread.": "Imewekwa alama kuwa haijasomwa.",
  "Matched exactly; addresses are stored only as a keyed hash.":
    "Imelinganishwa kikamilifu; anwani huhifadhiwa kama hash yenye ufunguo tu.",
  "Message HTML (sandboxed)": "HTML ya ujumbe (kwenye sandbox)",
  "Message ID or full recipient address, domains, senders, templates, API keys, mailboxes and pages.":
    "Kitambulisho cha ujumbe au anwani kamili ya mpokeaji, vikoa, watumaji, violezo, funguo za API, visanduku vya barua na kurasa.",
  "Message deleted.": "Ujumbe umefutwa.",
  "Message details": "Maelezo ya ujumbe",
  "Message metadata, delivery evidence, bounces, suppressions, captures and inbound mail (with its attachment files) for this recipient in this project and environment are removed. This cannot be undone.":
    "Metadata ya ujumbe, ushahidi wa utumaji, bounce, suppressions, captures na barua zinazoingia (pamoja na faili zake za viambatisho) za mpokeaji huyu katika mradi na mazingira haya zinaondolewa. Hili haliwezi kutenduliwa.",
  "Message states: queued, scheduled, submitting, submitted, retry_wait, failed, unknown, expired, captured (sandbox), cancelled. There is no “delivered” state.":
    "Hali za ujumbe: queued, scheduled, submitting, submitted, retry_wait, failed, unknown, expired, captured (sandbox), cancelled. Hakuna hali ya “delivered”.",
  "Messages by state": "Ujumbe kwa hali",
  "Messages sent from this project. States are the backend's own: “submitted” means the mail server accepted it.":
    "Ujumbe uliotumwa kutoka mradi huu. Hali ni za backend yenyewe: “submitted” inamaanisha seva ya barua ilikubali.",
  "Messages sent with it": "Ujumbe uliotumwa nao",
  "Messages waiting for their send time. Send one now or cancel it.":
    "Ujumbe unaosubiri muda wao wa kutumwa. Tuma mmoja sasa au ughairi.",
  "Messages your mail server has accepted but not yet handed to the recipient's server. Addresses are hidden here.":
    "Ujumbe ambao seva yako ya barua imekubali lakini haijakabidhi bado kwa seva ya mpokeaji. Anwani zimefichwa hapa.",
  Method: "Njia",
  "Missing sample values:": "Thamani za mfano zinazokosekana:",
  Mode: "Hali",
  "Move down": "Sogeza chini",
  "Move up": "Sogeza juu",
  "Moved to Inbox.": "Imehamishiwa kwenye Sanduku la barua.",
  "Must start with https://": "Lazima ianze na https://",
  Name: "Jina",
  "Name (internal)": "Jina (la ndani)",
  "Needs attention": "Inahitaji uangalizi",
  "Needs review": "Inahitaji ukaguzi",
  Never: "Kamwe",
  "New rules start switched off. Test one against a recent message before turning it on.":
    "Kanuni mpya huanza zikiwa zimezimwa. Zijaribu kwa ujumbe wa karibuni kabla ya kuziwasha.",
  "New signing secret": "Siri mpya ya kusaini",
  "Njiwa-Mail calls these HTTPS endpoints when something happens to your mail. Each call is signed.":
    "Njiwa-Mail huita endpoints hizi za HTTPS kitu kinapotokea kwa barua yako. Kila simu imesainiwa.",
  "Njiwa-Mail cannot see the server it hands mail to. Enter what your mail server's operator documents, so SPF and DKIM are checked against the real values. These do not change the connection settings.":
    "Njiwa-Mail haioni seva inayokabidhiwa barua. Weka kile mwendeshaji wa seva yako ya barua anachoandika kwenye nyaraka, ili SPF na DKIM zikaguliwe dhidi ya thamani halisi. Hizi hazibadilishi mipangilio ya muunganisho.",
  "Njiwa-Mail checks DNS. Your mail server must also be set up for this domain:":
    "Njiwa-Mail hukagua DNS. Seva yako ya barua lazima pia iwekwe kwa kikoa hiki:",
  "No activity recorded yet": "Hakuna shughuli iliyorekodiwa bado",
  "No bulk unsubscribes": "Hakuna kujiondoa kwa barua nyingi",
  "No captured mail": "Hakuna barua zilizonaswa",
  "No channels: alerts are only shown here.": "Hakuna chaneli: tahadhari huonyeshwa hapa tu.",
  "No deliveries": "Hakuna uwasilishaji",
  "No domain matches": "Hakuna kikoa kinachofanana",
  "No domains are claimed yet": "Hakuna vikoa vilivyodaiwa bado",
  "No drafts. Drafts are saved automatically while you write.":
    "Hakuna rasimu. Rasimu huhifadhiwa kiotomatiki unapoandika.",
  "No mailbox yet.": "Hakuna kisanduku bado.",
  "No messages from this domain in this environment.":
    "Hakuna ujumbe kutoka kikoa hiki kwenye mazingira haya.",
  "No messages match": "Hakuna ujumbe unaolingana",
  "No more events are sent to it. Pending deliveries are dropped.":
    "Hakuna matukio zaidi yanayotumwa kwake. Uwasilishaji unaosubiri unaondolewa.",
  "No open alerts": "Hakuna tahadhari zilizo wazi",
  "No other project has added it": "Hakuna mradi mwingine uliokiongeza",
  "No receiving domain yet. Add a domain with purpose “Receiving” and verify it, or set INBOUND_DOMAIN on the server.":
    "Hakuna kikoa cha kupokea bado. Ongeza kikoa chenye madhumuni “Receiving” na ukithibitishe, au weka INBOUND_DOMAIN kwenye seva.",
  "No recent mail": "Hakuna barua za karibuni",
  "No runs yet.": "Hakuna matumizi bado.",
  "No sender on a bulk domain": "Hakuna mtumaji kwenye kikoa cha barua nyingi",
  "No sender on this domain yet.": "Hakuna mtumaji kwenye kikoa hiki bado.",
  "No suppressed addresses": "Hakuna anwani zilizozuiwa",
  "No template — write the message": "Hakuna kiolezo — andika ujumbe",
  "No temporary inboxes": "Hakuna visanduku vya muda",
  "No verified sender address yet": "Hakuna anwani ya mtumaji iliyothibitishwa bado",
  "No webhook endpoints": "Hakuna endpoints za webhook",
  "No webhook events for this message.": "Hakuna matukio ya webhook kwa ujumbe huu.",
  "No worker, scheduler or inbound process has reported in the last hour.":
    "Hakuna mchakato wa worker, scheduler au inbound ulioripoti katika saa iliyopita.",
  "None recorded.": "Hakuna kilichorekodiwa.",
  "Not accepted (failed)": "Haukukubaliwa (umeshindwa)",
  "Not an email address:": "Si anwani ya barua pepe:",
  "Not chosen": "Haijachaguliwa",
  "Not passing": "Haifaulu",
  "Not proven yet": "Bado haijathibitishwa",
  "Not verified": "Haijathibitishwa",
  "Note recorded:": "Dokezo lililorekodiwa:",
  "Nothing found in this project and environment.":
    "Hakuna kilichopatikana katika mradi na mazingira haya.",
  "Nothing is sent or changed; the result is recorded as a test run.":
    "Hakuna kinachotumwa au kubadilishwa; matokeo yanarekodiwa kama jaribio.",
  "Nothing is sent until you press Start on the next page.":
    "Hakuna kinachotumwa hadi ubonyeze Start kwenye ukurasa unaofuata.",
  "Nothing matches these filters.": "Hakuna kinacholingana na vichujio hivi.",
  "Nothing recorded": "Hakuna kilichorekodiwa",
  "November service update": "Taarifa ya huduma ya Novemba",
  "Only open alerts": "Tahadhari zilizo wazi pekee",
  "Only verified sending domains of this project are listed.":
    "Vikoa vilivyothibitishwa vya kutuma vya mradi huu pekee ndivyo vimeorodheshwa.",
  "Open Dashboard →": "Fungua Dashboard →",
  "Open Unknown": "Fungua Unknown",
  "Open in an authenticator app on this device":
    "Fungua kwenye programu ya uthibitisho ya kifaa hiki",
  Operations: "Uendeshaji",
  "Order saved.": "Mpangilio umehifadhiwa.",
  "Outbound (SMTP submission)": "Inayotoka (uwasilishaji wa SMTP)",
  "Outbound connection": "Muunganisho unaotoka",
  "Outbound settings": "Mipangilio ya kutuma",
  "Owned by": "Inamilikiwa na",
  "Ownership changed. The new owner must run Check now on the domain.":
    "Umiliki umebadilika. Mmiliki mpya lazima aendeshe Check now kwenye kikoa.",
  Passed: "Imefaulu",
  "Paste addresses, or a CSV with an “email” column":
    "Bandika anwani, au CSV yenye safu ya “email”",
  "Paste the DKIM public key your mail server signs this domain with (its value starts with v=DKIM1). The DKIM check then compares the published record with it. Selector:":
    "Bandika ufunguo wa umma wa DKIM ambao seva yako ya barua hutumia kusaini kikoa hiki (thamani yake huanza na v=DKIM1). Ukaguzi wa DKIM kisha hulinganisha rekodi iliyochapishwa nao. Selector:",
  Path: "Njia",
  "Paused.": "Imesitishwa.",
  "Paused:": "Imesitishwa:",
  "People who can sign in. Installation operators also manage delivery infrastructure, domain ownership, users and settings.":
    "Watu wanaoweza kuingia. Waendeshaji wa usakinishaji pia husimamia miundombinu ya utumaji, umiliki wa vikoa, watumiaji na mipangilio.",
  Permissions: "Ruhusa",
  "Plain-text body": "Mwili wa maandishi matupu",
  Plan: "Mpango",
  "Policy bounces": "Policy bounces",
  Port: "Port",
  Postfix: "Postfix",
  Preview: "Hakikisho",
  Priority: "Kipaumbele",
  "Problems that need an operator: policy bounces, paused announcements, disabled webhooks, failing checks.":
    "Matatizo yanayohitaji mwendeshaji: policy bounces, matangazo yaliyositishwa, webhooks zilizozimwa, ukaguzi unaoshindwa.",
  Processes: "Michakato",
  "Profile added. Run its connection test before switching to it.":
    "Wasifu umeongezwa. Endesha jaribio lake la muunganisho kabla ya kubadilisha kwenda huo.",
  "Profile retired.": "Wasifu umestaafishwa.",
  "Profile saved. Its earlier connection test no longer counts; run it again.":
    "Wasifu umehifadhiwa. Jaribio lake la awali halihesabiwi tena; liendeshe tena.",
  "Project created with a Sandbox and a Live environment.":
    "Mradi umeundwa na mazingira ya Sandbox na Live.",
  "Project disabled.": "Mradi umezimwa.",
  "Project enabled.": "Mradi umewashwa.",
  "Proof that mail is signed": "Uthibitisho kuwa barua imetiwa saini",
  "Publish the new records first; activating earlier would make mail fail DKIM.":
    "Chapisha rekodi mpya kwanza; kuwasha mapema kungefanya barua ishindwe DKIM.",
  "Published templates only. Variables available: subject, sender, mailbox.":
    "Violezo vilivyochapishwa pekee. Vigezo vinavyopatikana: subject, sender, mailbox.",
  "Published version": "Toleo lililochapishwa",
  "Published versions can never be changed. Applications that do not pin a version start using it at once.":
    "Matoleo yaliyochapishwa hayabadiliki kamwe. Programu zisizobandika toleo huanza kuyatumia mara moja.",
  Queue: "Foleni",
  "Queued again.": "Imewekwa kwenye foleni tena.",
  "Queued for sending.": "Imewekwa kwenye foleni ya kutumwa.",
  "Queued from project": "Imewekwa kwenye foleni kutoka mradi",
  "Queued now.": "Imewekwa kwenye foleni sasa.",
  "Quick start": "Mwanzo wa haraka",
  Raised: "Imeibuliwa",
  "Rate per minute": "Kasi kwa dakika",
  "Re-queued.": "Imewekwa foleni tena.",
  "Read-write key from Mailcow → System → Configuration → Access → API.":
    "Ufunguo wa kusoma na kuandika kutoka Mailcow → System → Configuration → Access → API.",
  "Reason (required)": "Sababu (lazima)",
  Received: "Imepokelewa",
  "Received message, headers included": "Ujumbe uliopokelewa, pamoja na vichwa",
  Receiving: "Kupokea",
  "Recent messages": "Ujumbe wa karibuni",
  "Recipient (full address)": "Mpokeaji (anwani kamili)",
  Recipients: "Wapokeaji",
  "Recipients not reached yet will not get it, and queued messages are cancelled. This cannot be undone.":
    "Wapokeaji ambao hawajafikiwa bado hawatakipata, na ujumbe ulio kwenye foleni utaghairiwa. Hili haliwezi kutenduliwa.",
  "Record outcome": "Rekodi matokeo",
  "Record the real outcome": "Rekodi matokeo halisi",
  "Record this message as": "Rekodi ujumbe huu kama",
  "Records checked.": "Rekodi zimekaguliwa.",
  "Released.": "Imeachiliwa.",
  Remove: "Ondoa",
  "Remove operator access from": "Ondoa ufikiaji wa mwendeshaji kutoka kwa",
  "Remove the blocked addresses listed below.":
    "Ondoa anwani zilizozuiwa zilizoorodheshwa hapa chini.",
  "Remove the suppression for": "Ondoa suppression ya",
  "Remove those first.": "Ondoa hizo kwanza.",
  Replay: "Rudia",
  "Reply from": "Jibu kutoka",
  "Reply to": "Jibu kwa",
  "Requests above a limit get HTTP 429 with Retry-After. Bulk announcements have their own limits.":
    "Maombi yanayozidi kikomo hupata HTTP 429 na Retry-After. Matangazo ya barua nyingi yana vikomo vyake.",
  Required: "Lazima",
  "Reset two-step sign-in for": "Weka upya kuingia kwa hatua mbili kwa",
  "Restored.": "Imerejeshwa.",
  Result: "Matokeo",
  "Resumed.": "Imeendelezwa tena.",
  Retire: "Staafisha",
  "Retire this profile?": "Kustaafisha wasifu huu?",
  "Retry every deferred message now?": "Kujaribu tena kila ujumbe ulioahirishwa sasa?",
  "Retrying now.": "Inajaribu tena sasa.",
  "Reusable messages with variables, in English and Kiswahili. Published versions never change; usable from Compose, Bulk send and the API by slug.":
    "Ujumbe unaoweza kutumika tena wenye vigezo, kwa Kiingereza na Kiswahili. Matoleo yaliyochapishwa hayabadiliki kamwe; hutumika kutoka Compose, Bulk send na API kwa slug.",
  Review: "Kagua",
  Rollback: "Kurudisha nyuma",
  Rotate: "Badilisha",
  "Rotate secret": "Badilisha siri",
  "Rotate the signing secret?": "Kubadilisha siri ya kusaini?",
  "Rotate this key?": "Kubadilisha ufunguo huu?",
  "Rule deleted.": "Kanuni imefutwa.",
  "Rule turned off.": "Kanuni imezimwa.",
  "Rule turned on.": "Kanuni imewashwa.",
  "Rules that act on incoming mail, in order. Auto-replies never answer automatic mail and go at most once per sender per day.":
    "Kanuni zinazoshughulikia barua zinazoingia, kwa mpangilio. Majibu ya kiotomatiki hayajibu kamwe barua za kiotomatiki na hutumwa mara moja kwa mtumaji kwa siku.",
  "Run `njiwa-mail mta-agent` on the mail host and set MTA_AGENT_SOCKET and MTA_AGENT_TOKEN for the API (see Operations: Mail queue controls).":
    "Endesha `njiwa-mail mta-agent` kwenye seva ya barua na uweke MTA_AGENT_SOCKET na MTA_AGENT_TOKEN kwa API (angalia Operations: Mail queue controls).",
  "Run history": "Historia ya uendeshaji",
  "Run njiwa-mail installer apply -record on the mail host.":
    "Endesha njiwa-mail installer apply -record kwenye seva ya barua.",
  "Run the installer again so it creates their keys, then publish the DKIM records it prints.":
    "Endesha kisakinishi tena ili kiunde funguo zao, kisha chapisha rekodi za DKIM inazochapisha.",
  "Run this request. The sender is this project's default sender.":
    "Endesha ombi hili. Mtumaji ni mtumaji chaguomsingi wa mradi huu.",
  "SMTP host": "Mwenyeji wa SMTP",
  "SMTP is set in the server environment": "SMTP imewekwa kwenye mazingira ya seva",
  "SMTP_HOST is set in the server environment and takes precedence over the saved settings. A test only counts for the saved settings when both are identical.":
    "SMTP_HOST imewekwa kwenye mazingira ya seva na inatangulia mipangilio iliyohifadhiwa. Jaribio huhesabiwa kwa mipangilio iliyohifadhiwa tu pale yote mawili yanapofanana.",
  "SPF include": "SPF include",
  "Sandbox mail in this project. Nothing here reaches real recipients.":
    "Barua za Sandbox katika mradi huu. Hakuna kinachofika kwa wapokeaji halisi.",
  "Sandbox: the message is captured for review and nothing leaves the server.":
    "Sandbox: ujumbe unanaswa kwa ukaguzi na hakuna kinachotoka kwenye seva.",
  "Save a draft to preview it.": "Hifadhi rasimu ili kuiona hakikisho.",
  "Save as draft": "Hifadhi kama rasimu",
  "Save key": "Hifadhi ufunguo",
  "Save limits": "Hifadhi vikomo",
  "Save profile": "Hifadhi wasifu",
  "Save rule": "Hifadhi kanuni",
  "Save sending facts": "Hifadhi taarifa za utumaji",
  "Saved. Check each domain again to see the new requirements.":
    "Imehifadhiwa. Kagua kila kikoa tena kuona mahitaji mapya.",
  "Saved. Publish the records below and run the check again.":
    "Imehifadhiwa. Chapisha rekodi zilizo hapa chini na uendeshe ukaguzi tena.",
  "Saving withdraws this domain's SPF and DMARC pass until the check is run again, so Live mail pauses until the new records are published.":
    "Kuhifadhi huondoa ufaulu wa SPF na DMARC wa kikoa hiki hadi ukaguzi uendeshwe tena, kwa hiyo barua za Live zinasitishwa hadi rekodi mpya zichapishwe.",
  "Saving…": "Inahifadhi…",
  "Scheduled.": "Imeratibiwa.",
  "Search projects": "Tafuta miradi",
  "Search sender, subject or body": "Tafuta mtumaji, mada au mwili",
  "Secret rotated.": "Siri imebadilishwa.",
  "Send a message in Sandbox with Compose or with a sandbox API key.":
    "Tuma ujumbe kwenye Sandbox kwa Compose au kwa ufunguo wa API wa sandbox.",
  "Send a test": "Tuma jaribio",
  "Send again": "Tuma tena",
  "Send at": "Tuma saa",
  "Send at (optional)": "Tuma saa (si lazima)",
  "Send directly": "Tuma moja kwa moja",
  "Send later (optional)": "Tuma baadaye (si lazima)",
  "Send limits": "Vikomo vya kutuma",
  "Send limits for": "Vikomo vya kutuma vya",
  "Send test": "Tuma jaribio",
  "Send test to": "Tuma jaribio kwa",
  "Send the": "Tuma",
  "Send this message again?": "Kutuma ujumbe huu tena?",
  "Send through the queue": "Tuma kupitia foleni",
  "Send through the queue to test the real path: the message is accepted, queued and sent by the worker, and the result is recorded for these settings. Send directly for a quick check of the connection only.":
    "Tuma kupitia foleni ili kujaribu njia halisi: ujumbe unakubaliwa, unawekwa foleni na kutumwa na worker, na matokeo yanarekodiwa kwa mipangilio hii. Tuma moja kwa moja kwa ukaguzi wa haraka wa muunganisho pekee.",
  "Sender blocked.": "Mtumaji amezuiwa.",
  "Sender contains": "Mtumaji ana",
  "Sender removed.": "Mtumaji ameondolewa.",
  "Senders must be on a verified sending domain of this project.":
    "Watumaji lazima wawe kwenye kikoa cha kutuma kilichothibitishwa cha mradi huu.",
  Sending: "Kutuma",
  "Sending IP addresses or ranges": "Anwani za IP au safu za kutuma",
  "Sending and receiving": "Kutuma na kupokea",
  "Sending…": "Inatuma…",
  "Sent to the sandbox. Nothing left the server.":
    "Imetumwa kwenye sandbox. Hakuna kilichotoka kwenye seva.",
  "Separate addresses with commas.": "Tenganisha anwani kwa koma.",
  Service: "Huduma",
  "Service announcements to people who already use your product. Not for marketing to bought or scraped lists.":
    "Matangazo ya huduma kwa watu wanaotumia bidhaa yako tayari. Si kwa uuzaji kwa orodha zilizonunuliwa au zilizokwanguliwa.",
  "Set up in Mailcow": "Sanidi kwenye Mailcow",
  "Set up the queue agent to see Postfix and signer health here.":
    "Weka queue agent ili kuona afya ya Postfix na kitia saini hapa.",
  "Setup is complete. The Dashboard is now your landing page.":
    "Usanidi umekamilika. Dashboard sasa ndiyo ukurasa wako wa kuanzia.",
  "Shared templates are read-only here. Duplicate it to change it for this project.":
    "Violezo vya pamoja ni vya kusoma tu hapa. Vinakili ili kuvibadilisha kwa mradi huu.",
  Show: "Onyesha",
  "Show headers": "Onyesha vichwa",
  "Shown to recipients, for example “Njiwa Support”.":
    "Huonyeshwa kwa wapokeaji, kwa mfano “Njiwa Support”.",
  "Signed mail verified": "Barua iliyotiwa saini imethibitishwa",
  "Signing secret": "Siri ya kusaini",
  Slug: "Slug",
  "Slug for the copy": "Slug ya nakala",
  "Soft bounces": "Soft bounces",
  "Spam complaints from feedback loops, and bounces. A complaint suppresses the recipient and pauses any announcement it came from.":
    "Malalamiko ya spam kutoka feedback loops, na bounces. Malalamiko huzuia mpokeaji na husitisha tangazo lolote lililotoka kwake.",
  "Start sending?": "Kuanza kutuma?",
  "Start with a project. Each project has a Sandbox, where mail is captured, and a Live environment for real mail.":
    "Anza na mradi. Kila mradi una Sandbox, ambapo barua hunaswa, na mazingira ya Live kwa barua halisi.",
  "Started.": "Imeanzishwa.",
  "Stored encrypted; never shown again.": "Imehifadhiwa kwa usimbaji fiche; haionyeshwi tena.",
  "Subject contains": "Mada ina",
  "Subject or template": "Mada au kiolezo",
  Succeeded: "Imefaulu",
  Summary: "Muhtasari",
  Suppress: "Zuia",
  Suppressed: "Imezuiwa",
  Suppression: "Suppression",
  "Suppression removed.": "Suppression imeondolewa.",
  Switch: "Badilisha",
  "Switch mail delivery?": "Kubadilisha utumaji wa barua?",
  "Switch to": "Badilisha kwenda",
  "Switch to this profile": "Badilisha kwenda wasifu huu",
  "Template (optional)": "Kiolezo (si lazima)",
  "Template duplicated.": "Kiolezo kimenakiliwa.",
  "Template, not a record to copy: replace the part in <…> with your mail server's details before publishing.":
    "Kiolezo, si rekodi ya kunakili: badilisha sehemu iliyo ndani ya <…> kwa maelezo ya seva yako ya barua kabla ya kuchapisha.",
  "Temporary inbox created.": "Kisanduku cha muda kimeundwa.",
  "Temporary inboxes need an inbound domain. Set INBOUND_DOMAIN on the server and have your mail server relay it to Njiwa-Mail (see Delivery infrastructure).":
    "Visanduku vya muda vinahitaji kikoa cha inbound. Weka INBOUND_DOMAIN kwenye seva na uifanye seva yako ya barua isafirishe kwenda Njiwa-Mail (angalia Delivery infrastructure).",
  "Temporary password": "Nenosiri la muda",
  Test: "Jaribio",
  "Test against a recent message": "Jaribu kwa ujumbe wa karibuni",
  "Test and history": "Jaribio na historia",
  "Test captured in the Sandbox.": "Jaribio limenaswa kwenye Sandbox.",
  "Test queued.": "Jaribio limewekwa kwenye foleni.",
  "Test queued. The result appears above once the worker has sent it.":
    "Jaribio limewekwa kwenye foleni. Matokeo yataonekana hapo juu worker akishalituma.",
  "Test sender": "Mtumaji wa jaribio",
  "Testing…": "Inajaribu…",
  Text: "Maandishi",
  "That code was not accepted. Request failed": "Msimbo huo haukukubaliwa. Ombi limeshindwa",
  "The DKIM public key for each domain is entered on that domain's page.":
    "Ufunguo wa umma wa DKIM wa kila kikoa huwekwa kwenye ukurasa wa kikoa hicho.",
  "The Mailcow API is not configured, so do these steps in the Mailcow admin panel:":
    "API ya Mailcow haijasanidiwa, kwa hiyo fanya hatua hizi kwenye paneli ya usimamizi ya Mailcow:",
  "The SPF record must list the server that actually sends the mail (your mail server), not the server Njiwa-Mail runs on, unless they are the same.":
    "Rekodi ya SPF lazima iorodheshe seva inayotuma barua kweli (seva yako ya barua), si seva ambayo Njiwa-Mail inaendeshwa, isipokuwa ni ileile.",
  "The URL must start with https://": "URL lazima ianze na https://",
  "The address stops receiving mail, and its": "Anwani inaacha kupokea barua, na",
  "The body was removed by the retention policy.": "Mwili uliondolewa na sera ya uhifadhi.",
  "The draft could not be opened.": "Rasimu haikuweza kufunguliwa.",
  "The environment takes precedence: the test and the worker use it. The settings below apply when SMTP_HOST is not set.":
    "Mazingira yanatangulia: jaribio na worker huyatumia. Mipangilio iliyo hapa chini inatumika SMTP_HOST isipowekwa.",
  "The host checks run on the server itself and change nothing. Run this on the VPS, then reload this page:":
    "Ukaguzi wa mwenyeji hufanyika kwenye seva yenyewe na hauna mabadiliko. Endesha hii kwenye VPS, kisha pakia ukurasa huu tena:",
  "The installer has not run yet": "Kisakinishi hakijaendeshwa bado",
  "The mail server may or may not have accepted these. They are never retried automatically. Each row suggests an outcome from the evidence we hold; you decide.":
    "Seva ya barua inaweza kuwa imekubali au haikukubali hizi. Hazijaribiwi tena kiotomatiki. Kila safu inapendekeza matokeo kutoka kwenye ushahidi tulio nao; wewe ndiye unaamua.",
  "The mail server tries all deferred messages immediately.":
    "Seva ya barua inajaribu mara moja ujumbe wote ulioahirishwa.",
  "The message and its attachments are deleted permanently.":
    "Ujumbe na viambatisho vyake vinafutwa kabisa.",
  "The message, its events, delivery evidence and bounces are removed. This cannot be undone.":
    "Ujumbe, matukio yake, ushahidi wa utumaji na bounce vinaondolewa. Hili haliwezi kutenduliwa.",
  "The old secret stops working at once. Update your receiver with the new one.":
    "Siri ya zamani inaacha kufanya kazi mara moja. Sasisha kipokeaji chako kwa mpya.",
  "The project can send again.": "Mradi unaweza kutuma tena.",
  "The queue agent cannot be reached. Check that njiwa-mail-mta-agent is running.":
    "Queue agent haifikiwi. Hakikisha njiwa-mail-mta-agent inafanya kazi.",
  "The queue agent is not set up": "Queue agent haijasanidiwa",
  "The response is 202 with an id and status “queued”. Repeat the same Idempotency-Key to retry safely; it never sends twice.":
    "Jibu ni 202 lenye id na hali “queued”. Rudia Idempotency-Key ileile kujaribu tena kwa usalama; haitumi mara mbili kamwe.",
  "The rule and its run history are deleted.": "Kanuni na historia yake ya uendeshaji vinafutwa.",
  "The secret is shown once, right after you create it.":
    "Siri huonyeshwa mara moja, mara tu baada ya kuiunda.",
  "The slug is how applications choose this template.":
    "Slug ndiyo njia programu huchagua kiolezo hiki.",
  "The steps for this project. Each shows its real state; open any step directly. Sandbox works before setup is complete.":
    "Hatua za mradi huu. Kila moja inaonyesha hali yake halisi; fungua hatua yoyote moja kwa moja. Sandbox hufanya kazi kabla usanidi haujakamilika.",
  "The template must declare a text variable unsubscribe_url and show it; other variables come from CSV columns.":
    "Kiolezo lazima kitangaze kigezo cha maandishi unsubscribe_url na kukionyesha; vigezo vingine hutoka kwenye safu za CSV.",
  "The temporary password needs at least 12 characters.":
    "Nenosiri la muda linahitaji angalau herufi 12.",
  "The whole service, across all projects.": "Huduma nzima, katika miradi yote.",
  Then: "Kisha",
  "There is no circuit breaker: a temporary failure is retried per message with backoff; a message whose outcome is unknown is never retried automatically.":
    "Hakuna circuit breaker: hitilafu ya muda hujaribiwa tena kwa kila ujumbe kwa backoff; ujumbe usio na matokeo yanayojulikana haujaribiwi tena kiotomatiki kamwe.",
  "There is no project named": "Hakuna mradi uitwao",
  "These addresses will not receive mail:": "Anwani hizi hazitapokea barua:",
  "These verified sending domains have no DKIM key on the mail host yet:":
    "Vikoa hivi vilivyothibitishwa vya kutuma bado havina ufunguo wa DKIM kwenye seva ya barua:",
  "They are signed out and cannot sign in.": "Wanatolewa na hawawezi kuingia.",
  "They are signed out and sign in again with the new access.":
    "Wanatolewa na wanaingia tena kwa ufikiaji mpya.",
  "They can sign in again.": "Wanaweza kuingia tena.",
  "This domain has no DKIM key on your mail host yet. Run the installer again (Admin → Mail host shows the exact command), then publish the DKIM record it prints.":
    "Kikoa hiki bado hakina ufunguo wa DKIM kwenye seva yako ya barua. Endesha kisakinishi tena (Admin → Mail host inaonyesha amri kamili), kisha chapisha rekodi ya DKIM inayochapisha.",
  "This domain is for sending only.": "Kikoa hiki ni cha kutuma tu.",
  "This environment is disabled.": "Mazingira haya yamezimwa.",
  "This message answers": "Ujumbe huu unajibu",
  "This project has no mailbox yet. Create one to receive mail.":
    "Mradi huu hauna kisanduku bado. Unda kimoja kupokea barua.",
  "This template has no variables.": "Kiolezo hiki hakina vigezo.",
  Thread: "Mnyororo wa mazungumzo",
  Timeline: "Ratiba ya matukio",
  "To date": "Hadi tarehe",
  "Too many wrong codes. Try again in a few minutes. Request failed":
    "Misimbo mingi mibaya. Jaribu tena baada ya dakika chache. Ombi limeshindwa",
  "Transfer to project": "Hamisha kwenda mradi",
  Troubleshooting: "Utatuzi wa matatizo",
  Type: "Aina",
  "Type the queue ID to confirm": "Andika kitambulisho cha foleni kuthibitisha",
  Unclassified: "Haijaainishwa",
  "Unknown outcome": "Matokeo yasiyojulikana",
  "Unsaved changes. Preview shows the saved version.":
    "Mabadiliko hayajahifadhiwa. Hakikisho linaonyesha toleo lililohifadhiwa.",
  "Up to 7 days ahead, in your time zone.": "Hadi siku 7 mbele, kwa saa za eneo lako.",
  "Upload CSV": "Pakia CSV",
  "Use -record with the dkim-rotate commands so this page shows their state.":
    "Tumia -record na amri za dkim-rotate ili ukurasa huu uonyeshe hali yake.",
  "Use 1–64 letters, digits, dashes or underscores.":
    "Tumia herufi, tarakimu, vistari au mistari chini 1–64.",
  "Use a published template instead of writing the message.":
    "Tumia kiolezo kilichochapishwa badala ya kuandika ujumbe.",
  "Use a template": "Tumia kiolezo",
  "Use lowercase letters, digits, dot, dash, underscore or plus.":
    "Tumia herufi ndogo, tarakimu, nukta, kistari, mstari chini au plus.",
  "Use only for bulk announcements": "Tumia kwa matangazo ya barua nyingi pekee",
  "Use this when the device and recovery codes are lost. They are signed out and must set it up again.":
    "Tumia hii pale kifaa na misimbo ya kurejesha vimepotea. Wanatolewa na lazima waiweke tena.",
  "Use {{name}} for a CSV column. An unsubscribe link is added at the end.":
    "Tumia {{name}} kwa safu ya CSV. Kiungo cha kujiondoa huongezwa mwishoni.",
  "Used but not declared:": "Vimetumika lakini havijatangazwa:",
  "User added.": "Mtumiaji ameongezwa.",
  Username: "Jina la mtumiaji",
  Value: "Thamani",
  "Variable name": "Jina la kigezo",
  Variables: "Vigezo",
  "Variables from CSV:": "Vigezo kutoka CSV:",
  Verified: "Imethibitishwa",
  "Verify ownership first, then add senders.": "Thibitisha umiliki kwanza, kisha ongeza watumaji.",
  "Version history": "Historia ya matoleo",
  "View capture": "Tazama kilichonaswa",
  "Waiting to retry": "Inasubiri kujaribu tena",
  "Webhook endpoint": "Endpoint ya webhook",
  "What each record does, how to publish it, and what to do when a check fails.":
    "Kila rekodi hufanya nini, jinsi ya kuichapisha, na la kufanya ukaguzi unaposhindwa.",
  "What the installer did on your mail server, whether delivery evidence is arriving, and the state of DKIM keys. Changes are made with the installer on the server.":
    "Kisakinishi kilichofanya nini kwenye seva yako ya barua, kama ushahidi wa utumaji unafika, na hali ya funguo za DKIM. Mabadiliko hufanywa kwa kisakinishi kwenye seva.",
  "What was written": "Kilichoandikwa",
  "What your mail server sends from": "Seva yako ya barua hutuma kutoka wapi",
  When: "Lini",
  "When (all that are filled must match)": "Lini (vyote vilivyojazwa lazima vilingane)",
  "When mail arrives at": "Barua inapofika kwa",
  "Where alerts are sent": "Mahali tahadhari zinapotumwa",
  "Where your mail server relays received mail and bounces to Njiwa-Mail, as host:port on a private network. Used for the reachability check.":
    "Mahali seva yako ya barua inapotuma barua zilizopokelewa na bounce kwa Njiwa-Mail, kama host:port kwenye mtandao wa kibinafsi. Hutumika kwa ukaguzi wa kufikika.",
  "Which application uses it, for example “billing service”.":
    "Ni programu gani inaitumia, kwa mfano “billing service”.",
  "Whole number": "Nambari kamili",
  "Whole numbers only.": "Nambari kamili pekee.",
  "Why recipients were skipped or failed": "Kwa nini wapokeaji waliruka au walishindwa",
  "With the API, the Domains page can add domains, create DKIM keys and add the inbound transport for you. Without it, the Domains page shows the manual steps. Njiwa-Mail never changes global Mailcow settings.":
    "Kwa API, ukurasa wa Domains unaweza kuongeza vikoa, kuunda funguo za DKIM na kuongeza usafirishaji wa inbound kwa niaba yako. Bila hiyo, ukurasa wa Domains unaonyesha hatua za mkono. Njiwa-Mail haibadilishi kamwe mipangilio ya jumla ya Mailcow.",
  "Without Kiswahili content, Kiswahili requests fall back to English.":
    "Bila maudhui ya Kiswahili, maombi ya Kiswahili hurudi kwa Kiingereza.",
  "Write a message with Compose, or send from your application with an API key.":
    "Andika ujumbe kwa Compose, au tuma kutoka programu yako kwa ufunguo wa API.",
  "Write a message.": "Andika ujumbe.",
  "Write the message.": "Andika ujumbe.",
  "Written message": "Ujumbe ulioandikwa",
  "You will get the DNS records to publish next.": "Utapata rekodi za DNS za kuchapisha baadaye.",
  "Your mail server must also allow the SMTP login to send as this address. See Delivery infrastructure.":
    "Seva yako ya barua lazima pia iruhusu kuingia kwa SMTP kutuma kama anwani hii. Angalia Delivery infrastructure.",
  "Your mail server's DKIM key for this domain":
    "Ufunguo wa DKIM wa seva yako ya barua kwa kikoa hiki",
  "Your mail server's host name, for example mail.example.com.":
    "Jina la mwenyeji wa seva yako ya barua, kwa mfano mail.example.com.",
  "Your new API key": "Ufunguo wako mpya wa API",
  active: "inatumika",
  "address not set": "anwani haijawekwa",
  "an inbound message": "ujumbe unaoingia",
  and: "na",
  "and all mail it received are deleted now.": "na barua zote ilizopokea zinafutwa sasa.",
  "any mailbox": "kisanduku chochote",
  archive: "hifadhi",
  archived: "imehifadhiwa kwenye kumbukumbu",
  "auto-reply with": "jibu la kiotomatiki lenye",
  "bulk only": "barua nyingi pekee",
  "call a webhook": "ita webhook",
  "can be active at once.": "vinaweza kutumika kwa wakati mmoja.",
  "catch-all": "catch-all",
  clear: "futa",
  current: "ya sasa",
  day: "siku",
  default: "chaguomsingi",
  "domain(s) were verified by more than one project before this version. No project can send Live mail from them until you assign each one.":
    "kikoa(vikoa) kilithibitishwa na zaidi ya mradi mmoja kabla ya toleo hili. Hakuna mradi unaoweza kutuma barua za Live kutoka huko hadi uvikabidhi kimoja kimoja.",
  draft: "rasimu",
  "draft only": "rasimu pekee",
  "environment. A Sandbox key can never send real mail.":
    "mazingira. Ufunguo wa Sandbox hauwezi kutuma barua halisi kamwe.",
  "for real (Live).": "kwa kweli (Live).",
  "forward to": "peleka kwa",
  "full name": "jina kamili",
  "has an attachment": "ina kiambatisho",
  "has attachments": "ina viambatisho",
  healthy: "nzuri",
  "host:port, for example njiwa-inbound:2525": "host:port, kwa mfano njiwa-inbound:2525",
  "in 24 hours": "ndani ya saa 24",
  "in the Sandbox (captured only).": "kwenye Sandbox (zinanaswa tu).",
  "installer not run yet": "kisakinishi hakijaendeshwa bado",
  last: "ya mwisho",
  "last checked": "ilikaguliwa mwisho",
  "last test failed": "jaribio la mwisho limeshindwa",
  "last test passed": "jaribio la mwisho limefaulu",
  message: "ujumbe",
  messages: "ujumbe",
  no: "hapana",
  "no host": "hakuna mwenyeji",
  "no — see Mail server": "hapana — angalia Seva ya barua",
  none: "hakuna",
  "not matched": "haijalingana",
  "not on this environment's approved recipient list":
    "haiko kwenye orodha ya wapokeaji walioidhinishwa ya mazingira haya",
  "not published": "haijachapishwa",
  "not set": "haijawekwa",
  "not tested": "haijajaribiwa",
  "not verified": "haijathibitishwa",
  "nothing yet": "hakuna bado",
  off: "imezimwa",
  on: "imewashwa",
  "only PDF, PNG, JPEG and plain text files can be attached.":
    "faili za PDF, PNG, JPEG na maandishi matupu pekee ndizo zinaweza kuambatishwa.",
  "or you cannot open it. Check the link, or pick a project you can use.":
    "au huwezi kuifungua. Angalia kiungo, au chagua mradi unaoweza kutumia.",
  "our message": "ujumbe wetu",
  "pending Live messages through the new profile. Their sender domains may need new SPF/DKIM records for it.":
    "ujumbe wa Live unaosubiri kupitia wasifu mpya. Vikoa vya watumaji wao vinaweza kuhitaji rekodi mpya za SPF/DKIM kwa huo.",
  "per minute": "kwa dakika",
  "previous profile, for rollback": "wasifu wa awali, kwa kurudisha nyuma",
  published: "imechapishwa",
  "queue ID": "kitambulisho cha foleni",
  "re-checking every minute while this page is open":
    "inakagua tena kila dakika ukurasa huu ukiwa wazi",
  "recipient is suppressed": "mpokeaji amezuiwa",
  "recipients will get this announcement": "wapokeaji watapata tangazo hili",
  required: "lazima",
  revision: "toleo",
  runs: "uendeshaji",
  "sender contains": "mtumaji ana",
  "sent message": "ujumbe uliotumwa",
  shared: "vya pamoja",
  silent: "kimya",
  since: "tangu",
  "spam score": "alama ya spam",
  starts: "huanza",
  "stored messages and their attachments are deleted.":
    "ujumbe uliohifadhiwa na viambatisho vyake vinafutwa.",
  "subject contains": "mada ina",
  suppressed: "imezuiwa",
  tested: "imejaribiwa",
  "unique valid addresses": "anwani halali za kipekee",
  unread: "haijasomwa",
  "view the message": "tazama ujumbe",
  weak: "dhaifu",
  "will belong to": "itakuwa ya",
  "will no longer be stored in this environment, and their messages here are archived.":
    "haitahifadhiwa tena kwenye mazingira haya, na ujumbe wao hapa unawekwa kwenye kumbukumbu.",
  "with selector": "na selector",
  "with the Njiwa-Mail inbound listener as next hop.":
    "ikiwa na inbound listener ya Njiwa-Mail kama hatua inayofuata.",
  "with the “Send written messages” permission.": "na ruhusa ya “Send written messages”.",
  yes: "ndiyo",
  you: "wewe",
  "“submitted” means the mail server accepted the message. Njiwa-Mail does not claim delivery to the inbox.":
    "“submitted” inamaanisha seva ya barua ilikubali ujumbe. Njiwa-Mail haidai kuwa umefika kwenye sanduku la barua.",
};

Object.assign(swExtra, swDns);
Object.assign(swExtra, swSetup);
