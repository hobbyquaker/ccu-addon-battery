import json, sys
S = sys.argv[1]
EQ = "https://www.eq-3.de/Downloads/eq3/downloads_produktkatalog/homematic/bda/%s_UM_GE_eQ-3_web.pdf"
HIP = "https://homematic-ip.com/sites/default/files/downloads/"
ELV = "https://media.elv.com/file/"
def b(n, t): return [{"count": n, "type": t}]
M = {
 # BidCos-RF, eQ-3 manuals
 "HM-CC-RT-DN":      ("battery", b(2,"AA"),  "eq3-manual", EQ % "HM-CC-RT-DN", "2x 1,5V LR6/Mignon/AA", 1.5, None),
 "HM-TC-IT-WM-W-EU": ("battery", b(2,"AAA"), "eq3-manual", EQ % "HM-TC-IT-WM-W-EU", "2 x 1,5 V LR03/Micro/AAA", None, None),
 "HM-Sec-SCo":       ("battery", b(1,"AAA"), "eq3-manual", EQ % "HM-Sec-SCo", "1x 1,5 V LR03/Micro/AAA", None, None),
 "HM-Sec-SC-2":      ("battery", b(2,"LR44"),"eq3-manual", EQ % "HM-Sec-SC-2", "2x 1,5 V LR44", None, None),
 "HM-Sec-RHS":       ("battery", b(2,"LR44"),"eq3-manual", EQ % "HM-Sec-RHS", "Power supply: 2 x button cell LR44", 2, None),
 "HM-Sec-TiS":       ("battery", b(1,"CR2032"),"eq3-manual", "https://www.eq-3.de/downloads/download/homematic/bda/hm-sec-tis_um_ge_web.pdf", "Power supply: 1 x lithium coin cell CR 2032", None, None),
 "HM-Sec-WDS-2":     ("battery", b(2,"AA"),  "eq3-manual", EQ % "HM-Sec-WDS-2", "Power supply: 2 x 1.5 V LR6 batteries (mignon/AA)", None, None),
 "HM-Sec-Key":       ("battery", b(3,"AA"),  "eq3-manual", EQ % "HM-Sec-Key", "3x 1,5 V LR6/Mignon/AA", None, "the manual covers HM-Sec-Key and HM-Sec-Key-S"),
 "HM-Sec-Key-S":     ("battery", b(3,"AA"),  "eq3-manual", EQ % "HM-Sec-Key", "3x 1,5 V LR6/Mignon/AA", None, "the HM-Sec-Key manual covers HM-Sec-Key-S"),
 "HM-Sec-MDIR-3":    ("battery", b(3,"AA"),  "eq3-manual", EQ % "HM-Sec-MDIR-3", "3x 1,5 V LR6/Mignon/AA", None, None),
 "HM-Sec-Sir-WM":    ("battery", b(2,"C"),   "eq3-manual", EQ % "HM-Sec-Sir-WM", "2x 1,5 V LR14/Baby/C", None, None),
 "HM-Sec-Win":       ("rechargeable", [{"count": 1, "type": "pack", "note": "WinMatic battery pack, 10.8 VDC"}], "eq3-manual", EQ % "HM-Sec-Win", "Power supply: battery pack (10.8 VDC)", None, None),
 "HM-SCI-3-FM":      ("battery", b(1,"CR2032"),"eq3-manual", EQ % "HM-SCI-3-FM", "Power supply: 1x lithium coin cell, type CR 2032", None, None),
 "HM-PBI-4-FM":      ("battery", b(1,"CR2032"),"eq3-manual", EQ % "HM-PBI-4-FM", "Power supply: 1x Lithium button cell CR2032", None, None),
 "HM-SwI-3-FM":      ("battery", b(1,"CR2032"),"elv-manual", ELV + "76785_um.pdf", "Power supply: 1 x Lithium button cell CR 2032", None, None),
 "HM-PB-2-WM55":     ("battery", b(2,"AAA"), "eq3-manual", "https://www.eq-3.de/downloads/download/homematic/bda/hm-pb-2-wm55_um_ge_eq-3_web.pdf", "Batterien: 2x Micro / AAA / LR03", None, None),
 "HM-PB-2-WM55-2":   ("battery", b(2,"AAA"), "eq3-manual", EQ % "HM-PB-2-WM55-2", "Batterien: 2x Micro / AAA / LR03", None, None),
 "HM-PB-6-WM55":     ("battery", b(2,"AAA"), "eq3-manual", EQ % "HM-PB-6-WM55", "2 x 1,5 V LR03/Micro AAA", None, None),
 "HM-PB-4Dis-WM-2":  ("battery", b(3,"AAA"), "eq3-manual", EQ % "HM-PB-4Dis-WM-2", "3x 1,5 V LR03/Micro/AAA", None, None),
 "HM-RC-4-3":        ("battery", b(1,"AAA"), "eq3-manual", EQ % "HM-RC-4-3", "1 x 1,5 V LR03/Micro/AAA", None, None),
 "HM-RC-8":          ("battery", b(2,"AAA"), "eq3-manual", EQ % "HM-RC-8", "2x 1,5 V LR03/Micro/AAA", None, None),
 "HM-Sen-MDIR-O-2":  ("battery", b(3,"AA"),  "eq3-manual", EQ % "HM-Sen-MDIR-O-2", "3x 1,5 V LR6/Mignon/AA", None, None),
 "HM-Sen-MDIR-O-3":  ("battery", b(3,"AA"),  "eq3-manual", "https://www.eq-3.de/downloads/download/homematic/bda/HM-Sen-MDIR-O-3_UM_GE_eQ-3_web.pdf", "3x 1,5 V LR6/Mignon/AA", None, None),
 "HM-WDS10-TH-O":    ("battery", b(2,"AA"),  "eq3-manual", EQ % "HM-WDS10-TH-O", "2x 1,5 V LR6/ Mignon/AA", None, None),
 "HM-WDS30-T-O":     ("battery", b(2,"AAA"), "eq3-manual", EQ % "HM-WDS30-T-O", "Power supply: 2 x LR03 (Micro)", None, None),
 "HM-WDS40-TH-I-2":  ("battery", b(2,"AA"),  "eq3-manual", EQ % "HM-WDS40-TH-I-2", "2 x 1,5 V LR6/Mignon/AA", None, None),
 "HM-OU-CFM-TW":     ("battery", b(2,"C"),   "eq3-manual", EQ % "HM-OU-CFM-TW", "2x 1,5 V LR14/Baby/C", None, None),
 "HM-WDC7000":       ("battery-or-dc", b(4,"AA"), "eq3-manual", EQ % "HM-WDC7000", "Basisstation (Hauptversorgung) 7,5 V DC über Steckernetzteil; Basisstation (Notbetrieb) 4 x Batterie LR6/Mignon/AA", None, "runs on the mains adapter; the 4 cells are the backup"),
 "HM-LC-Sw1-Ba-PCB": ("battery-or-dc", None, "eq3-manual", EQ % "HM-LC-Sw1-Ba-PCB", "Spannungsversorgung: Batteriebetrieb, 5 - 15 VDC", None, "a board for a battery of the builder's choice (5-15 V); no defined slot"),
 "HM-LC-Sw4-WM":     ("mains", None, "eq3-manual", EQ % "HM-LC-Sw4-WM", "Spannungsversorgung: 7–15 VDC", None, "an external DC supply"),
 "HM-OU-CFM-Pl":     ("mains", None, "eq3-manual", EQ % "HM-OU-CFM-Pl", "Versorgungsspannung: 230 V/50 Hz", None, None),
 "HM-OU-LED16":      ("mains", None, "eq3-manual", EQ % "HM-OU-LED16", "Versorgungsspannung: 7,5 VDC", None, None),
 "HM-RC-2-PBU-FM":   ("mains", None, "eq3-manual", EQ % "HM-RC-2-PBU-FM", "Versorgungsspannung: 230 V/50 Hz", None, None),
 # HmIP, older models (eQ-3 manuals via homematic-ip.com downloads or ELV)
 "HMIP-SWDO":        ("battery", b(1,"AAA"), "elv-manual", ELV + "140733_um.pdf", "1x 1,5 V LR03/Micro/AAA", 2, None),
 "HmIP-SWDO-PL":     ("battery", b(2,"AAA"), "elv-manual", ELV + "153734_um.pdf", "2x 1,5 V LR03/Micro/AAA", 4, None),
 "HmIP-SWDO-A":      ("battery", b(1,"AAA"), "eq3-manual", HIP + "157857a0_160027a0_hmip-swdo-2_um.pdf", "1x 1,5 V LR03/Micro/AAA", 2, None),
 "HmIP-SMI55":       ("battery", b(2,"AAA"), "elv-manual", ELV + "151769_um.pdf", "2x 1,5 V LR03/Micro/AAA", None, None),
 "HmIP-SMI55-A":     ("battery", b(2,"AAA"), "eq3-manual", HIP + "hmip-smi55-a-um-web.pdf", "2x 1,5 V LR03/Micro/AAA", 4, None),
 "HmIP-SMO-A-2":     ("battery", b(2,"AA"),  "eq3-manual", HIP + "hmip-smo-a-2-um-web.pdf", "2x 1,5 V LR6/Mignon/AA", None, None),
 "HmIP-STHO-A":      ("battery", b(2,"AA"),  "eq3-manual", HIP + "hmip-stho-a_um_web.pdf", "2x 1,5 V LR6/Mignon/AA", 5, None),
 "HmIP-SWD":         ("battery", b(2,"AAA"), "eq3-manual", HIP + "hmip-swd_um_web.pdf", "2x 1,5 V LR03/Micro/AAA", 5, None),
 "HmIP-KRC4":        ("battery", b(1,"AAA"), "eq3-manual", HIP + "hmip-krc4_um_web.pdf", "1x 1,5 V LR03/Micro/AAA", 2, None),
 "HmIP-KRCA":        ("battery", b(1,"AAA"), "eq3-manual", HIP + "hmip-krca_um_web.pdf", "1x 1,5 V LR03/Micro/AAA", 2, None),
 "HmIP-KRCK":        ("battery", b(1,"AAA"), "eq3-manual", HIP + "hmip-krck_um_web.pdf", "1x 1,5 V LR03/Micro/AAA", 2, None),
 "HMIP-WRC2":        ("battery", b(2,"AAA"), "eq3-manual", HIP + "hmip-wrc2_um_web.pdf", "2x 1,5 V LR03/Micro/AAA", 4, None),
 "HmIP-WRC2-A":      ("battery", b(2,"AAA"), "eq3-manual", HIP + "hmip-wrc2-a-um-gef-eq-3.pdf", "2x 1,5 V LR03/Micro/AAA", 4, None),
 "HmIP-WRC2-A-2":    ("battery", b(2,"AAA"), "eq3-manual", HIP + "161830a0_161869a0_hmip-wrc2-a-2_um_g.pdf", "2x 1,5 V LR03/Micro/AAA", 4, None),
 "HmIP-WRC6-A":      ("battery", b(2,"AAA"), "eq3-manual", HIP + "hmip-wrc6-a-um-gef-eq-3.pdf", "2x 1,5 V LR03/Micro/AAA", 4, None),
 "HmIP-WTH-A":       ("battery", b(2,"AAA"), "eq3-manual", HIP + "156669a0_159820a0_hmip-wth-1-um.pdf", "2x 1,5 V LR03/Micro/AAA", 2, None),
 "HmIP-eTRV-B":      ("battery", b(2,"AA"),  "eq3-manual", HIP + "hmip-etrv-b_um_web.pdf", "2x 1,5 V LR6/Mignon/AA", None, None),
 "HmIP-eTRV-C":      ("battery", b(2,"AA"),  "eq3-manual", HIP + "hmip-etrv-c-151239a0-produktdatenblatt.pdf", "2x 1,5 V LR06/Mignon/AA", None, "from the product datasheet (article 151239A0)"),
 "HmIP-eTRV-E-A":    ("battery", b(2,"AA"),  "eq3-manual", HIP + "155105a0_156650a0_158419a0_hmip-etrv-e_um.pdf", "2x 1,5 V LR6/Mignon/AA", 2, None),
 "HmIP-ASIR":        ("battery", b(3,"AA"),  "elv-manual", ELV + "142801_um.pdf", "3x 1.5 V LR6/mignon/AA", 2, None),
 "HmIP-SWSD-2-NL":   ("battery", [{"count": 2, "type": "fixed", "replaceable": False}], "eq3-manual", HIP + "161884a0_hmip-swsd-2-nl_um_gefn.pdf", "2x fest verbaute 3 V-Lithium-Batterien", 10, None),
 "HmIP-STE2-PCB":    ("mains", None, "elv-manual", ELV + "155614_um.pdf", "Versorgungsspannung: 12 VDC (max. 50 mA) 12 VAC/50 Hz (max. 50 mA)", None, "an external 12 V supply per the manual - check a device"),
 "HmIP-BRC2":        ("mains", None, "eq3-manual", HIP + "hmip-brc2_um_web.pdf", "Versorgungsspannung: 230 V/50 Hz", None, None),
 "HmIP-WSS":         ("rechargeable", [{"count": 3, "type": "AAA", "chemistry": "nimh", "note": "backup cells; runs on the supplied mains adapter"}], "eq3-manual", HIP + "160980a0_hmip-wss_um_g.pdf", "3× 1,2 V HR03/Micro/AAA, 1× Netzteil", None, "mains adapter with rechargeable backup cells"),
 "ELV-SH-SMSI":      ("battery", b(2,"AA"),  "elv-manual", ELV + "161792_um.pdf", "2x 1,5 V LR6/Mignon/AA", None, None),
 "HM-WDS100-C6-O-2": ("battery", b(3,"AA"),  "elv-manual", ELV + "132192_um.pdf", "3x 1,5 V LR6/Mignon/AA", None, None),
 "HM-Sec-SD-2":      ("battery", [{"count": 2, "type": "fixed", "replaceable": False}], "eq3-manual", "https://www.eq-3.de/downloads/download/homematic/bda/hm-sec-sd-2_um_eq-3_ge_web.pdf", "Spannungsversorgung: 2x fest verbaute 3 V Lithium-Batterie", 10, None),
 "HM-Sec-SD":        ("battery", b(3,"AA"),  "eq3-manual", "https://www.eq-3.de/Downloads/eq3/downloads_produktkatalog/homematic/bda/HM-Sec-SD_GE_UM_V1.5_150407.pdf", "Spannungsversorgung: 4,5 V/3 x LR 6", None, None),
 "HM-Sec-SFA-SM":    ("rechargeable", [{"count": 1, "type": "lead12V", "note": "lead-gel back-up battery 12 V 1.2 Ah; runs on 230 V"}], "elv-manual", ELV + "84392_um.pdf", "Power supply: 230 V/50 Hz, max. 30 mA Back-up supply: Lead gel battery, 12 V, 1.2 Ah", None, "mains with a rechargeable back-up; LOW_BAT_LIMIT and BAT_DEFECT_LIMIT watch the back-up battery"),
 "HMIP-PS":          ("mains", None, "eq3-manual", HIP + "hmip-ps_um_web.pdf", "Versorgungsspannung: 230 V/50 Hz", None, None),
 "HMIP-PSM":         ("mains", None, "eq3-manual", HIP + "hmip-psm-um-web.pdf", "Versorgungsspannung: 230 V/50 Hz", None, None),
 "HmIP-PSM-CH":      ("mains", None, "eq3-manual", HIP + "hmip-psm-ch_um_gefi_web.pdf", "Versorgungsspannung: 230 V/50 Hz", None, None),
 "HmIP-FSI16":       ("mains", None, "eq3-manual", HIP + "hmip-fsi16-um-web.pdf", "Versorgungsspannung: 230 V/50 Hz", None, None),
}
out = {}
for k, (p, sl, src, url, q, life, note) in M.items():
    out[k] = {"power": p, "slots": sl, "src": src, "url": url, "quote": q}
    if life: out[k]["life"] = life
    if note: out[k]["note"] = note
json.dump(out, open(f"{S}/manual.json", "w"), indent=1, ensure_ascii=False)
print(len(out))
