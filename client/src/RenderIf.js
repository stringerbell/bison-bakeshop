export default function RenderIf(props) {
  if (props.condition) {
    return props.children
  }
  return props.fallback
};
