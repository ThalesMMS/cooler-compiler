class Main inherits IO {
  nothing : Object;
  main() : Object { case nothing of o : Object => out_string("unreachable\n"); esac };
};
