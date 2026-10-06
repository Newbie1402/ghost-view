---
name: GhostView
description: A quiet, legible window onto public social content.
colors:
  accent: "#315b4b"
  accent-soft: "#eaf0e9"
  bg: "#f8f9f5"
  surface: "#fff"
  text: "#222a26"
  muted: "#667069"
  line: "#e0e5dc"
  focus: "#6c957d"
typography:
  display:
    fontFamily: 'Manrope, "Avenir Next", sans-serif'
    fontSize: "clamp(36px, 5vw, 60px)"
    fontWeight: 650
    lineHeight: 1.08
    letterSpacing: "-0.038em"
  headline:
    fontFamily: 'Manrope, "Avenir Next", sans-serif'
    fontSize: "30px"
    fontWeight: 600
    lineHeight: 1.13
    letterSpacing: "-0.03em"
  title:
    fontFamily: 'Manrope, "Avenir Next", sans-serif'
    fontSize: "24px"
    fontWeight: 600
    letterSpacing: "-0.02em"
  body:
    fontFamily: '"Avenir Next", Avenir, "Segoe UI", sans-serif'
    fontSize: "15px"
    lineHeight: 1.5
  label:
    fontFamily: '"Avenir Next", Avenir, "Segoe UI", sans-serif'
    fontSize: "12px"
    fontWeight: 600
rounded:
  panel: "16px"
  content: "12px"
  control: "8px"
  badge: "5px"
  circle: "50%"
spacing:
  small: "8px"
  medium: "12px"
  content: "16px"
  section: "24px"
  wide: "32px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.surface}"
    rounded: "{rounded.control}"
    padding: "0 22px"
  button-icon:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.circle}"
    size: "40px"
  search-field:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.content}"
    padding: "7px 7px 7px 17px"
  platform-selected:
    backgroundColor: "{colors.accent-soft}"
    textColor: "{colors.accent}"
    rounded: "9px"
    padding: "10px 20px"
  badge:
    backgroundColor: "{colors.accent-soft}"
    textColor: "{colors.accent}"
    typography: "{typography.label}"
    rounded: "{rounded.badge}"
    padding: "3px 7px"
  candidate-card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.content}"
    padding: "20px"
  state-panel:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.panel}"
    padding: "34px"
---

# Design System: GhostView

## Overview

**Creative North Star: "The Light Library Index"**

GhostView uses an all-light default interface: warm paper, white working surfaces, green-gray ink and restrained forest-green actions. Spacious composition and short, readable labels make discovery feel quiet and deliberate.

Availability is part of the visual hierarchy. Demo badges, access states and supported resource tabs stay readable beside the content they describe. Media artwork supplies visual variety; interface decoration stays modest. An explicit theme toggle also provides a secondary dark theme through the existing CSS variable overrides.

**Key Characteristics:**

- Light neutral surfaces with restrained green controls.
- Manrope headings and a familiar humanist body stack.
- Rounded working surfaces, fine dividers and open whitespace.
- Visible availability labels and focused media viewing.

## Colors

The palette combines warm near-white surfaces with low-saturation green-gray text and controls.

### Primary

- **Forest Green** (`accent`): primary actions, selected tabs, verification marks and one emphasized headline phrase.
- **Pale Leaf** (`accent-soft`): selected platform backgrounds, hover surfaces, media placeholders and skeletons.

### Neutral

- **Warm Paper** (`bg`): the page and full-screen viewer canvas.
- **White Working Surface** (`surface`): search, candidate and state panels; circular controls.
- **Green-Gray Ink** (`text`): headings and readable primary information.
- **Quiet Gray** (`muted`): descriptions, captions, usernames and secondary statistics.
- **Soft Divider** (`line`): field strokes, section separators and inactive control borders.
- **Focus Green** (`focus`): the visible keyboard focus outline.

**The Availability Rule.** Status labels use text as well as color; a private or unavailable state must remain understandable without recognizing the palette.

## Typography

**Display Font:** locally hosted variable Manrope, with Avenir Next and sans-serif fallbacks.

**Body Font:** Avenir Next, Avenir, Segoe UI, sans-serif.

**Character:** compact geometric headings meet familiar body text. Tight heading tracking gives the wordmark and titles a shared voice; body copy retains normal tracking.

### Hierarchy

- **Display:** hero headline; the responsive overrides use 52px below the tablet breakpoint and 38px on mobile.
- **Headline:** introductory section title; mobile uses 28px. Profile titles use the neighboring 31px desktop / 24px mobile step.
- **Title:** result and state headings; smaller mobile result headings use 20px.
- **Body:** default reading text. Hero supporting copy uses 16px with a relaxed 1.6 line height; profile and descriptive copy use 12–14px where space is limited.
- **Label:** capability badges, captions and controls. Search input text is 16px on mobile.

**The Reading Rule.** Keep provider descriptions and biographies as plain text with natural wrapping. Numeric statistics use tabular figures; unavailable counts receive no invented numeric value.

## Layout

The centered shell is at most 1160px wide, increasing to 1190px at the wide-screen breakpoint. Desktop gutters total 80px, tablet gutters 48px and mobile gutters 36px. The search working surface is capped at 734px and result content at 1040px.

The reusable rhythm uses the spacing tokens for control gaps, card padding and section separation. The gallery changes from four columns to five at 1440px, to three below 900px and to two below 600px. Media cells maintain a 4:5 portrait frame; story cells use 9:16. On mobile, platform overview columns become divided rows and the profile heading remains compact.

The native media dialog uses the full viewport height. Its media area flexes between the header and controls; mobile previous/next controls overlay the lower sides of the media frame.

## Elevation & Depth

Fine borders and tonal surfaces provide most structure. One soft ambient shadow lifts the search working surface from the canvas (`0 16px 48px -20px #263e2928`). Candidate cards and gallery entries remain flat. The dialog backdrop darkens the context while the viewer itself retains its theme surface.

**The Quiet Depth Rule.** Reserve ambient elevation for the search surface; use existing dividers and tonal changes for other interface grouping.

## Shapes

Large working and state panels use the panel radius. Search fields, candidate rows and desktop media frames use the content radius; buttons and playback controls use the control radius. Small availability badges use the badge radius. Avatars and compact icon actions are circular. Mobile media frames tighten to 9px, and viewer media to 6px.

## Components

### Buttons

Primary actions pair Forest Green with White Working Surface text. Hover brightens the surface; keyboard focus uses the shared offset outline. Secondary viewer controls have a white surface, fine border and pale hover fill. Circular icon actions use the same bordered surface treatment. Disabled controls reduce opacity and remove the action cursor.

### Inputs / Fields

The search field is a bordered white row with a search icon, flexible input and attached primary action. Focus changes the outer border to Forest Green. Mobile input text stays at 16px. Keep the input's accessible label even when the visible placeholder is brief.

### Navigation

Platform selectors use muted text at rest and Pale Leaf plus Forest Green when selected. Resource tabs use a bottom divider and an accent underline for the active tab. Selection is also exposed through native accessibility attributes.

### Chips

Availability badges combine Pale Leaf fill, Forest Green text and the Soft Divider stroke. They communicate provider or demo status with readable text rather than decorative symbols.

### Cards / Containers

Candidate rows combine a circular avatar, wrapping identity text and a platform label. Hover changes the border and fill together. State panels center a short heading, plain explanation and retry action. Gallery entries place actions below the media frame rather than covering the content with a permanent toolbar.

### Media Viewer

The full-screen dialog presents contained media, previous/next controls, a caption and explicitly labeled playback/download controls. Keep image and video proportions intact. The media hover scale is modest; skeleton animation and all transitions stop under reduced-motion preferences.

## Do's and Don'ts

### Do:

- **Do** use the light palette as the default and existing theme variables for alternate surfaces.
- **Do** keep availability labels adjacent to the resource they explain.
- **Do** preserve visible keyboard focus, wrapping identity text and reduced-motion support.
- **Do** use the shared radii and spacing before introducing a new step.

### Don't:

- **Don't** introduce a dark header into the all-light default interface.
- **Don't** replace status wording with color alone or fabricated counts.
- **Don't** add heavy shadows to every content card.
- **Don't** render provider HTML or turn media artwork into decorative interface chrome.
