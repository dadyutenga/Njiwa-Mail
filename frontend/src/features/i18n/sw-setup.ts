// Kiswahili for the setup journey and host discovery. Keys are the exact English
// text passed to phrase(), including the server's step details and reasons.
// Needs a native-speaker review, like the rest of the Kiswahili.
export const swSetup: Record<string, string> = {
  "Before you continue": "Kabla ya kuendelea",
  "Go to this step": "Nenda kwenye hatua hii",
  "Mail ports in use": "Milango ya barua inayotumika",
  "Mail servers Njiwa-Mail can connect to": "Seva za barua ambazo Njiwa-Mail inaweza kuunganisha",
  "Mail services": "Huduma za barua",
  "Not possible on this server: it already runs a mail service or cannot run one. The installer will refuse and change nothing.":
    "Haiwezekani kwenye seva hii: tayari inaendesha huduma ya barua au haiwezi kuendesha moja. Kisakinishi kitakataa na hakitabadilisha chochote.",
  "Nothing on this server is changed or replaced.":
    "Hakuna kitu kwenye seva hii kinachobadilishwa wala kuondolewa.",
  Recommended: "Inapendekezwa",
  "Recommended for this server": "Inapendekezwa kwa seva hii",
  "Run the read-only scan on the server so Njiwa-Mail can recommend a delivery mode. It changes nothing.":
    "Endesha uchunguzi wa kusoma tu kwenye seva ili Njiwa-Mail ipendekeze njia ya kutuma. Haubadilishi chochote.",
  Scanned: "Ilichunguzwa",
  Server: "Seva",
  "Settings filled in. Enter the mailbox login, save, then test.":
    "Mipangilio imejazwa. Weka kuingia kwa sanduku la barua, hifadhi, kisha jaribu.",
  "Setup is complete.": "Usanidi umekamilika.",
  Step: "Hatua",
  "Use these settings": "Tumia mipangilio hii",
  "What is on this server": "Kilichopo kwenye seva hii",
  "Why Njiwa-Mail will not install its own mail server here":
    "Kwa nini Njiwa-Mail haitasakinisha seva yake ya barua hapa",
  "You can stop at any point and continue later, from any device.":
    "Unaweza kusimama wakati wowote na kuendelea baadaye, kutoka kifaa chochote.",
  "in use": "inatumika",
  "none found": "hakuna kilichopatikana",
  of: "kati ya",
  // Step titles and states.
  "Install Njiwa-Mail": "Sakinisha Njiwa-Mail",
  "Detect what is already on the server": "Gundua kilichopo tayari kwenye seva",
  "Choose how mail is sent": "Chagua jinsi barua zinavyotumwa",
  "Connect the mail server": "Unganisha seva ya barua",
  "Add and verify a sending domain": "Ongeza na thibitisha kikoa cha kutuma",
  "Publish the DNS records": "Chapisha rekodi za DNS",
  "Create a sender address": "Unda anwani ya mtumaji",
  "Create an API key": "Unda ufunguo wa API",
  "Send a test email": "Tuma barua pepe ya majaribio",
  "Open the dashboard": "Fungua dashibodi",
  Done: "Imekamilika",
  "To do": "Inasubiri",
  Skipped: "Imerukwa",
  // Server details shown in the steps.
  "The API and dashboard are running.": "API na dashibodi zinafanya kazi.",
  "Run the read-only scan on the server. It finds mail servers (Mailcow, Postfix, Exim), busy ports, proxies and certificates, and changes nothing.":
    "Endesha uchunguzi wa kusoma tu kwenye seva. Unatafuta seva za barua (Mailcow, Postfix, Exim), milango inayotumika, proksi na vyeti, na haubadilishi chochote.",
  "The server was scanned; the recommendation is shown below.":
    "Seva imechunguzwa; pendekezo linaonyeshwa hapa chini.",
  "Choose a delivery mode first.": "Chagua njia ya kutuma kwanza.",
  "Delivery is configured and its checks pass.": "Utumaji umesanidiwa na ukaguzi wake umepita.",
  "Create a project first (Projects page).": "Unda mradi kwanza (ukurasa wa Miradi).",
  "Add your domain and publish its ownership TXT record.":
    "Ongeza kikoa chako na uchapishe rekodi yake ya TXT ya umiliki.",
  "A sending domain is verified and claimed by this project.":
    "Kikoa cha kutuma kimethibitishwa na kinamilikiwa na mradi huu.",
  "SPF, DKIM and DMARC pass.": "SPF, DKIM na DMARC zimepita.",
  "Add a Live sender on the verified domain (for example noreply@your-domain).":
    "Ongeza mtumaji wa Live kwenye kikoa kilichothibitishwa (kwa mfano noreply@kikoa-chako).",
  "A Live sender address exists.": "Anwani ya mtumaji wa Live ipo.",
  "Create a Live API key for your application.": "Unda ufunguo wa API wa Live kwa programu yako.",
  "A Live API key exists.": "Ufunguo wa API wa Live upo.",
  "Finish the steps above.": "Kamilisha hatua zilizo juu.",
  "Setup is complete. Messages, delivery evidence and analytics are on the dashboard.":
    "Usanidi umekamilika. Ujumbe, ushahidi wa utumaji na takwimu ziko kwenye dashibodi.",
  "You were signed out. This happens when the session expires, or after a change to the password, role, operator access or two-step sign-in of this account, or a sign-in with a recovery code.":
    "Umetolewa. Hii hutokea muda wa kikao unapoisha, au baada ya kubadilisha nenosiri, jukumu, ruhusa ya mwendeshaji au uthibitishaji wa hatua mbili wa akaunti hii, au kuingia kwa msimbo wa kurejesha.",
  "Steps 5 to 9 are for project": "Hatua 5 hadi 9 ni za mradi",
};
