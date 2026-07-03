/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#eef6ff',
          100: '#d9eaff',
          200: '#bcdaff',
          300: '#8ec1ff',
          400: '#599eff',
          500: '#327bff',
          600: '#1d5cf3',
          700: '#1747dc',
          800: '#173cad',
          900: '#193785',
        },
      },
    },
  },
  plugins: [],
}