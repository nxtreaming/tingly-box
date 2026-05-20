import { createTheme } from '@mui/material/styles';
import type { ResolvedThemeMode } from './types';
import { baseTypography, baseShape, baseComponents } from './base';
import { lightPalette } from './palettes/light';
import { darkPalette } from './palettes/dark';
import { lightComponents } from './components/light';
import { darkComponents } from './components/dark';

const THEME_REGISTRY = {
  light: { palette: lightPalette, components: lightComponents },
  dark: { palette: darkPalette, components: darkComponents },
} as const;

const createAppTheme = (mode: ResolvedThemeMode) => {
  const { palette, components } = THEME_REGISTRY[mode];
  const textColors = palette.text as { primary: string; secondary: string; disabled: string };

  return createTheme({
    palette: palette as any,
    typography: baseTypography(textColors.primary, textColors.secondary, textColors.disabled),
    shape: baseShape,
    components: {
      ...baseComponents,
      ...components,
    },
  });
};

export default createAppTheme;
export type { ResolvedThemeMode, ThemeMode } from './types';
